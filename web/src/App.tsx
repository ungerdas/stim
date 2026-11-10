import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { api } from './api';
import type { InstanceConfig, InstanceStatus, KindInfo, Snapshot, State, Torrent, TransferJob } from './types';
import { count, rate } from './format';
import { connectionProblem } from './text';
import { InstanceDialog } from './components/InstanceDialog';
import { MoveDialog } from './components/MoveDialog';
import { StatsDialog } from './components/StatsDialog';
import { DeleteDialog } from './components/DeleteDialog';
import { TransferPanel } from './components/TransferPanel';
import { Sidebar, type StateFilter, type TrackerCount } from './components/Sidebar';
import { TorrentTable, torrentKey, type Sort, type SortKey } from './components/TorrentTable';

const POLL_MS = 3000;
const TRANSFER_POLL_MS = 1200;
const DISMISSED_KEY = 'stim.dismissedTransfers';
const HIDDEN_TRACKERS_KEY = 'stim.hiddenTrackers';

// Small per-browser settings. Storage can be unavailable or hold junk, so
// both directions fail quietly.
function loadSet(key: string): Set<string> {
  try {
    const value: unknown = JSON.parse(localStorage.getItem(key) ?? '[]');
    return new Set(Array.isArray(value) ? value.filter((v): v is string => typeof v === 'string') : []);
  } catch {
    return new Set();
  }
}

function saveSet(key: string, set: Set<string>, keep = Infinity) {
  try {
    localStorage.setItem(key, JSON.stringify([...set].slice(-keep)));
  } catch {
    // Not saved; the choice still applies until the page is reloaded.
  }
}
const INSTANCE_COLORS = 6; // --inst-0 … --inst-5 in styles.css

type DialogState = { existing: InstanceConfig | null } | null;

const TEXT_KEYS: SortKey[] = ['name', 'instance', 'state', 'label', 'tracker'];

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
  const [moveOpen, setMoveOpen] = useState(false);
  const [statsOpen, setStatsOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [jobs, setJobs] = useState<TransferJob[]>([]);
  const [dismissed, setDismissed] = useState<Set<string>>(() => loadSet(DISMISSED_KEY));
  // Trackers the user has unticked. Storing the hidden ones (rather than the
  // shown ones) means a tracker that appears later is shown, not silently
  // filtered out.
  const [hiddenTrackers, setHiddenTrackers] = useState<Set<string>>(() => loadSet(HIDDEN_TRACKERS_KEY));
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

  // Moves: poll quickly while one is running, otherwise along with the list.
  const loadJobs = useCallback(async () => {
    try {
      setJobs(await api.transfers());
    } catch {
      // The banner for the main list already reports an unreachable server.
    }
  }, []);
  const anyRunning = jobs.some((j) => j.finishedAt === 0);
  useEffect(() => {
    loadJobs();
    const timer = window.setInterval(loadJobs, anyRunning ? TRANSFER_POLL_MS : POLL_MS * 4);
    return () => window.clearInterval(timer);
  }, [loadJobs, anyRunning]);
  // Show the result of a finished move in the list right away.
  const wasRunning = useRef(false);
  useEffect(() => {
    if (wasRunning.current && !anyRunning) refresh();
    wasRunning.current = anyRunning;
  }, [anyRunning, refresh]);

  const dismissJob = (id: string) =>
    setDismissed((prev) => {
      const next = new Set(prev).add(id);
      saveSet(DISMISSED_KEY, next, 50);
      return next;
    });

  const updateHiddenTrackers = (update: (prev: Set<string>) => Set<string>) =>
    setHiddenTrackers((prev) => {
      const next = update(prev);
      saveSet(HIDDEN_TRACKERS_KEY, next);
      return next;
    });
  const toggleTracker = (name: string) =>
    updateHiddenTrackers((prev) => {
      const next = new Set(prev);
      if (next.has(name)) next.delete(name);
      else next.add(name);
      return next;
    });
  const onlyTracker = (name: string) =>
    updateHiddenTrackers(() => new Set(torrents.map((t) => t.tracker).filter((t) => t !== name)));
  const showAllTrackers = () => updateHiddenTrackers(() => new Set());
  const visibleJobs = jobs.filter((j) => j.finishedAt === 0 || !dismissed.has(j.id));

  const cancelJob = async (id: string) => {
    try {
      await api.cancelTransfer(id);
      await loadJobs();
    } catch (e) {
      setNotice({ text: (e as Error).message, bad: true });
    }
  };

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

  // Tracker counts follow the client filter but not the tracker filter
  // itself, so an unticked tracker still shows how many torrents it has.
  const trackerCounts = useMemo((): TrackerCount[] => {
    const counts = new Map<string, number>();
    for (const t of inInstance) counts.set(t.tracker, (counts.get(t.tracker) ?? 0) + 1);
    // Keep an unticked tracker in the list, so it can be ticked again, as long
    // as some client still has it.
    const known = new Set(torrents.map((t) => t.tracker));
    for (const h of hiddenTrackers) if (known.has(h) && !counts.has(h)) counts.set(h, 0);
    return [...counts]
      .map(([name, n]) => ({ name, count: n }))
      .sort(
        (a, b) =>
          Number(a.name === '') - Number(b.name === '') || b.count - a.count || a.name.localeCompare(b.name),
      );
  }, [inInstance, torrents, hiddenTrackers]);

  const inTracker = useMemo(
    () => (hiddenTrackers.size === 0 ? inInstance : inInstance.filter((t) => !hiddenTrackers.has(t.tracker))),
    [inInstance, hiddenTrackers],
  );

  const stateCounts = useMemo(() => {
    const counts = {
      all: inTracker.length,
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
    for (const t of inTracker) {
      counts[t.state]++;
      if (t.downloadRate > 0 || t.uploadRate > 0) counts.active++;
    }
    return counts;
  }, [inTracker]);

  const visible = useMemo(() => {
    const terms = query.toLowerCase().split(/\s+/).filter(Boolean);
    const list = inTracker.filter((t) => {
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
  }, [inTracker, stateFilter, query, sort, instanceById]);

  const summary = useMemo(() => {
    let uploaded = 0;
    let downloaded = 0;
    for (const t of visible) {
      uploaded += t.uploaded;
      downloaded += t.downloaded;
    }
    return { uploaded, downloaded, filtered: visible.length !== torrents.length };
  }, [visible, torrents.length]);

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

  const closeDialog = useCallback(() => setDialog(null), []);
  const closeMove = useCallback(() => setMoveOpen(false), []);
  const closeStats = useCallback(() => setStatsOpen(false), []);
  const closeDelete = useCallback(() => setDeleteOpen(false), []);

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
        trackers={trackerCounts}
        hiddenTrackers={hiddenTrackers}
        onToggleTracker={toggleTracker}
        onOnlyTracker={onlyTracker}
        onShowAllTrackers={showAllTrackers}
        summary={summary}
        onOpenStats={() => setStatsOpen(true)}
      />

      <main className="main">
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

        <TransferPanel
          jobs={visibleJobs}
          instances={instances}
          colorOf={colorOf}
          onCancel={cancelJob}
          onDismiss={dismissJob}
        />

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
            <button
              type="button"
              className="btn"
              onClick={() => setMoveOpen(true)}
              disabled={selectedTorrents.length === 0 || instances.length < 2}
              title={instances.length < 2 ? 'Legg til en klient til for å kunne flytte' : undefined}
            >
              Flytt til …
            </button>
            <button
              type="button"
              className="btn"
              onClick={() => setDeleteOpen(true)}
              disabled={selectedTorrents.length === 0}
            >
              Slett …
            </button>
          </div>
          <p className="toolbar-count" aria-live="polite">
            {selectedTorrents.length > 0
              ? `${count(selectedTorrents.length)} valgt`
              : filtered
                ? `${count(visible.length)} av ${count(torrents.length)} torrenter`
                : `${count(torrents.length)} torrenter`}
          </p>
          <dl className="totals">
            <div className="total">
              <dt>
                <span aria-hidden="true">↓</span> Ned
              </dt>
              <dd>{rate(totalDown) || '0 B/s'}</dd>
            </div>
            <div className="total">
              <dt>
                <span aria-hidden="true">↑</span> Opp
              </dt>
              <dd>{rate(totalUp) || '0 B/s'}</dd>
            </div>
          </dl>
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
                    showAllTrackers();
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
          onClose={closeDialog}
          onChanged={(text) => {
            setDialog(null);
            setNotice({ text });
            refresh();
          }}
        />
      )}

      {moveOpen && (
        <MoveDialog
          kinds={kinds}
          instances={instances}
          colorOf={colorOf}
          torrents={selectedTorrents}
          onClose={closeMove}
          onStarted={(job) => {
            setMoveOpen(false);
            setSelected(new Set());
            setJobs((prev) => [job, ...prev.filter((j) => j.id !== job.id)]);
          }}
          onEditInstance={(id) => {
            setMoveOpen(false);
            openEdit(id);
          }}
        />
      )}

      {deleteOpen && (
        <DeleteDialog
          torrents={selectedTorrents}
          allTorrents={torrents}
          instances={instances}
          colorOf={colorOf}
          onClose={closeDelete}
          onDone={({ removed, deleteData, failed }) => {
            setDeleteOpen(false);
            const failedNames = Object.keys(failed).map((id) => instanceById.get(id)?.status.name ?? id);
            if (failedNames.length > 0) {
              setNotice({
                text: `Fikk ikke slettet på ${failedNames.join(', ')}: ${Object.values(failed)[0]}`,
                bad: true,
              });
            } else {
              const noun = removed === 1 ? 'torrent' : 'torrenter';
              setNotice({
                text: deleteData
                  ? `Slettet ${count(removed)} ${noun} og filene`
                  : `Fjernet ${count(removed)} ${noun}. Filene ligger igjen.`,
              });
            }
            setSelected(new Set());
            refresh();
          }}
        />
      )}

      {statsOpen && (
        <StatsDialog
          instances={instances}
          torrents={torrents}
          kinds={kinds}
          colorOf={colorOf}
          onClose={closeStats}
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
