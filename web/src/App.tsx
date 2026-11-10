import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { api } from './api';
import type { InstanceConfig, InstanceStatus, KindInfo, Snapshot, State, Torrent } from './types';
import { count, rate } from './format';
import { connectionProblem } from './text';
import { FishGlyph } from './components/Fish';
import { InstanceDialog } from './components/InstanceDialog';
import { Shoal } from './components/Shoal';
import { Sidebar, type StateFilter } from './components/Sidebar';
import { TorrentTable, torrentKey, type Sort, type SortKey } from './components/TorrentTable';

const POLL_MS = 3000;
const INSTANCE_COLORS = 6; // --inst-0 … --inst-5 in styles.css

type DialogState = { existing: InstanceConfig | null } | null;

const TEXT_KEYS: SortKey[] = ['name', 'instance', 'state', 'label'];

export default function App() {
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [kinds, setKinds] = useState<KindInfo[]>([]);
  const [instanceFilter, setInstanceFilter] = useState('all');
  const [stateFilter, setStateFilter] = useState<StateFilter>('all');
  const [query, setQuery] = useState('');
  const [sort, setSort] = useState<Sort>({ key: 'addedAt', dir: 'desc' });
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [dialog, setDialog] = useState<DialogState>(null);
  const [notice, setNotice] = useState<{ text: string; bad?: boolean } | null>(null);
  const [acting, setActing] = useState(false);
  const lastClicked = useRef<string | null>(null);

  // --- data -----------------------------------------------------------

  const refresh = useCallback(async () => {
    try {
      setSnapshot(await api.torrents());
      setLoadError(null);
    } catch (e) {
      setLoadError((e as Error).message);
    }
  }, []);

  useEffect(() => {
    api.kinds().then(setKinds).catch(() => undefined);
  }, []);

  useEffect(() => {
    let timer: number | undefined;
    let stopped = false;
    const tick = async () => {
      if (!document.hidden) await refresh();
      if (!stopped) timer = window.setTimeout(tick, POLL_MS);
    };
    tick();
    const onVisible = () => {
      if (!document.hidden) refresh();
    };
    document.addEventListener('visibilitychange', onVisible);
    return () => {
      stopped = true;
      window.clearTimeout(timer);
      document.removeEventListener('visibilitychange', onVisible);
    };
  }, [refresh]);

  useEffect(() => {
    if (!notice) return;
    const t = window.setTimeout(() => setNotice(null), notice.bad ? 9000 : 4000);
    return () => window.clearTimeout(t);
  }, [notice]);

  const instances = useMemo(() => snapshot?.instances ?? [], [snapshot]);
  const torrents = useMemo(() => snapshot?.torrents ?? [], [snapshot]);

  const instanceById = useMemo(() => {
    const map = new Map<string, { status: InstanceStatus; color: string }>();
    instances.forEach((status, i) => map.set(status.id, { status, color: `var(--inst-${i % INSTANCE_COLORS})` }));
    return map;
  }, [instances]);
  const colorOf = useCallback((id: string) => instanceById.get(id)?.color ?? 'var(--ink-soft)', [instanceById]);
  const instanceOf = useCallback((id: string) => instanceById.get(id)?.status, [instanceById]);

  // A filter pointing at a removed client would show an empty list forever.
  useEffect(() => {
    if (instanceFilter !== 'all' && snapshot && !instanceById.has(instanceFilter)) setInstanceFilter('all');
  }, [instanceFilter, instanceById, snapshot]);

  // --- filtering and sorting -------------------------------------------

  const inInstance = useMemo(
    () => (instanceFilter === 'all' ? torrents : torrents.filter((t) => t.instanceId === instanceFilter)),
    [torrents, instanceFilter],
  );

  const stateCounts = useMemo(() => {
    const counts = {
      all: inInstance.length,
      active: 0,
      downloading: 0,
      seeding: 0,
      paused: 0,
      queued: 0,
      checking: 0,
      moving: 0,
      error: 0,
      unknown: 0,
    } satisfies Record<StateFilter, number>;
    for (const t of inInstance) {
      counts[t.state]++;
      if (t.downloadRate > 0 || t.uploadRate > 0) counts.active++;
    }
    return counts;
  }, [inInstance]);

  const visible = useMemo(() => {
    const terms = query.toLowerCase().split(/\s+/).filter(Boolean);
    const list = inInstance.filter((t) => {
      if (stateFilter === 'active') {
        if (t.downloadRate <= 0 && t.uploadRate <= 0) return false;
      } else if (stateFilter !== 'all' && t.state !== stateFilter) {
        return false;
      }
      if (terms.length === 0) return true;
      const haystack = `${t.name} ${t.label} ${t.tags.join(' ')} ${t.tracker}`.toLowerCase();
      return terms.every((term) => haystack.includes(term));
    });

    const dir = sort.dir === 'asc' ? 1 : -1;
    const collator = new Intl.Collator('nb', { numeric: true, sensitivity: 'base' });
    const value = (t: Torrent): string | number => {
      switch (sort.key) {
        case 'instance':
          return instanceById.get(t.instanceId)?.status.name ?? '';
        case 'eta':
          // Unknown and finished sort last when ascending.
          return t.eta < 0 ? Number.MAX_SAFE_INTEGER : t.eta;
        default:
          return t[sort.key];
      }
    };
    return [...list].sort((a, b) => {
      const va = value(a);
      const vb = value(b);
      const cmp =
        typeof va === 'string' || typeof vb === 'string'
          ? collator.compare(String(va), String(vb))
          : va - vb;
      return cmp !== 0 ? cmp * dir : collator.compare(a.name, b.name) || a.hash.localeCompare(b.hash);
    });
  }, [inInstance, stateFilter, query, sort, instanceById]);

  const onSort = (key: SortKey) =>
    setSort((s) =>
      s.key === key
        ? { key, dir: s.dir === 'asc' ? 'desc' : 'asc' }
        : { key, dir: TEXT_KEYS.includes(key) ? 'asc' : 'desc' },
    );

  // --- selection --------------------------------------------------------

  const onToggle = useCallback(
    (key: string, range: boolean) => {
      setSelected((prev) => {
        const next = new Set(prev);
        const keys = visible.map(torrentKey);
        const from = lastClicked.current ? keys.indexOf(lastClicked.current) : -1;
        const to = keys.indexOf(key);
        if (range && from >= 0 && to >= 0) {
          const [lo, hi] = from < to ? [from, to] : [to, from];
          for (let i = lo; i <= hi; i++) next.add(keys[i]);
        } else if (next.has(key)) {
          next.delete(key);
        } else {
          next.add(key);
        }
        return next;
      });
      lastClicked.current = key;
    },
    [visible],
  );

  const onToggleAll = () =>
    setSelected((prev) => {
      const keys = visible.map(torrentKey);
      const allSelected = keys.length > 0 && keys.every((k) => prev.has(k));
      const next = new Set(prev);
      for (const k of keys) {
        if (allSelected) next.delete(k);
        else next.add(k);
      }
      return next;
    });

  // Only torrents that still exist count as selected.
  const selectedTorrents = useMemo(() => torrents.filter((t) => selected.has(torrentKey(t))), [torrents, selected]);

  const act = async (action: 'pause' | 'resume') => {
    if (selectedTorrents.length === 0) return;
    setActing(true);
    try {
      const { failed } = await api.act(
        action,
        selectedTorrents.map((t) => ({ instanceId: t.instanceId, hash: t.hash })),
      );
      const failedNames = Object.keys(failed).map((id) => instanceById.get(id)?.status.name ?? id);
      if (failedNames.length > 0) {
        setNotice({ text: `Fikk ikke gjort det på ${failedNames.join(', ')}: ${Object.values(failed)[0]}`, bad: true });
      } else {
        const n = selectedTorrents.length;
        const noun = n === 1 ? 'torrent' : 'torrenter';
        setNotice({ text: action === 'pause' ? `Pauset ${n} ${noun}` : `Gjenopptok ${n} ${noun}` });
        setSelected(new Set());
      }
      await refresh();
    } catch (e) {
      setNotice({ text: (e as Error).message, bad: true });
    } finally {
      setActing(false);
    }
  };

  // --- clients ----------------------------------------------------------

  const openEdit = async (id: string) => {
    try {
      const all = await api.instances();
      const existing = all.find((i) => i.id === id);
      if (existing) setDialog({ existing });
    } catch (e) {
      setNotice({ text: (e as Error).message, bad: true });
    }
  };

  const totalDown = instances.reduce((n, i) => n + i.downloadRate, 0);
  const totalUp = instances.reduce((n, i) => n + i.uploadRate, 0);
  const offline = instances.filter((i) => !i.online);
  const kindLabel = (kind: string) => kinds.find((k) => k.kind === kind)?.label ?? kind;
  const filtered = visible.length !== torrents.length;

  return (
    <div className="app">
      <Sidebar
        instances={instances}
        kinds={kinds}
        colorOf={colorOf}
        total={torrents.length}
        instanceFilter={instanceFilter}
        onInstanceFilter={setInstanceFilter}
        stateFilter={stateFilter}
        stateCounts={stateCounts}
        onStateFilter={(s: State | 'all' | 'active') => setStateFilter(s)}
        onAdd={() => setDialog({ existing: null })}
        onEdit={openEdit}
      />

      <main className="main">
        <header className="topbar">
          <Shoal
            torrents={torrents}
            colorOf={colorOf}
            onPick={(t) => {
              setInstanceFilter('all');
              setStateFilter('all');
              setQuery(t.name);
            }}
          />
          <dl className="totals">
            <div className="total">
              <dt>
                <FishGlyph hollow />
                Ned
              </dt>
              <dd>{rate(totalDown) || '0 B/s'}</dd>
            </div>
            <div className="total">
              <dt>
                <FishGlyph />
                Opp
              </dt>
              <dd>{rate(totalUp) || '0 B/s'}</dd>
            </div>
          </dl>
        </header>

        {loadError && (
          <p className="banner banner-bad" role="alert">
            Får ikke kontakt med Stim-serveren ({loadError}). Prøver igjen om noen sekunder.
          </p>
        )}
        {offline.map((inst) => (
          <p key={inst.id} className="banner banner-bad" role="alert">
            <strong>{inst.name}:</strong> {connectionProblem(inst.errorCode, kindLabel(inst.kind), inst.error)}
            {inst.errorCode === 'auth' && ' Stim venter med å prøve på nytt, så klienten ikke sperrer adressen.'}
            {inst.count > 0 && ' Listen viser det klienten sist rapporterte.'}{' '}
            <button type="button" className="link" onClick={() => openEdit(inst.id)}>
              Endre klienten
            </button>
            {inst.errorCode === 'auth' && (
              <>
                {' eller '}
                <button
                  type="button"
                  className="link"
                  onClick={() => api.retryInstance(inst.id).then(refresh).catch(() => undefined)}
                >
                  prøv igjen
                </button>
              </>
            )}
          </p>
        ))}

        <div className="toolbar">
          <input
            type="search"
            className="search"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Søk i navn, etikett eller tracker"
            aria-label="Søk i torrenter"
          />
          <div className="toolbar-actions">
            <button
              type="button"
              className="btn"
              onClick={() => act('pause')}
              disabled={selectedTorrents.length === 0 || acting}
            >
              Pause
            </button>
            <button
              type="button"
              className="btn"
              onClick={() => act('resume')}
              disabled={selectedTorrents.length === 0 || acting}
            >
              Gjenoppta
            </button>
          </div>
          <p className="toolbar-count" aria-live="polite">
            {selectedTorrents.length > 0
              ? `${count(selectedTorrents.length)} valgt`
              : filtered
                ? `${count(visible.length)} av ${count(torrents.length)} torrenter`
                : `${count(torrents.length)} torrenter`}
          </p>
        </div>

        {snapshot === null && !loadError ? (
          <p className="empty">Henter torrenter …</p>
        ) : instances.length === 0 ? (
          <div className="empty">
            <h1 className="empty-title">Ingen klienter ennå</h1>
            <p>Legg til en qBittorrent- eller Deluge-instans, så samler Stim torrentene dine i én liste.</p>
            <button type="button" className="btn btn-primary" onClick={() => setDialog({ existing: null })}>
              Legg til klient
            </button>
          </div>
        ) : visible.length === 0 ? (
          <div className="empty">
            {torrents.length === 0 ? (
              <p>Klientene har ingen torrenter ennå.</p>
            ) : (
              <>
                <p>Ingen torrenter passer med søket og filtrene.</p>
                <button
                  type="button"
                  className="btn"
                  onClick={() => {
                    setQuery('');
                    setStateFilter('all');
                    setInstanceFilter('all');
                  }}
                >
                  Vis alle torrenter
                </button>
              </>
            )}
          </div>
        ) : (
          <TorrentTable
            torrents={visible}
            instanceOf={instanceOf}
            colorOf={colorOf}
            selected={selected}
            onToggle={onToggle}
            onToggleAll={onToggleAll}
            sort={sort}
            onSort={onSort}
          />
        )}
      </main>

      {dialog && (
        <InstanceDialog
          kinds={kinds}
          existing={dialog.existing}
          onClose={() => setDialog(null)}
          onChanged={(text) => {
            setDialog(null);
            setNotice({ text });
            refresh();
          }}
        />
      )}

      {notice && (
        <div className={`toast${notice.bad ? ' toast-bad' : ''}`} role="status">
          <span>{notice.text}</span>
          <button type="button" className="toast-close" onClick={() => setNotice(null)} aria-label="Lukk melding">
            ×
          </button>
        </div>
      )}
    </div>
  );
}
