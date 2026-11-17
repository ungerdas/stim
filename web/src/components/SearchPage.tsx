import { useEffect, useMemo, useRef, useState } from 'react';
import { api } from '../api';
import type { InstanceStatus, SearchHit, SearchIndexer, Torrent } from '../types';
import { age, bytes, count } from '../format';
import { looseName, sameSize } from '../releases';
import { torrentKey } from '../duplicates';

interface Props {
  instances: InstanceStatus[];
  torrents: Torrent[];
  colorOf: (instanceId: string) => string;
  onBack: () => void;
  onOpenSettings: () => void;
  onAdded: (text: string) => void;
}

type SortKey = 'title' | 'indexer' | 'size' | 'seeders' | 'leechers' | 'grabs' | 'age';

const HIDDEN_INDEXERS_KEY = 'stim.searchHiddenIndexers';
const LAST_GRAB_KEY = 'stim.lastGrab';

const CATEGORIES: { label: string; ids: number[] }[] = [
  { label: 'Alt', ids: [] },
  { label: 'TV', ids: [5000] },
  { label: 'Film', ids: [2000] },
];

function loadJSON<T>(key: string, fallback: T): T {
  try {
    const v = localStorage.getItem(key);
    return v ? (JSON.parse(v) as T) : fallback;
  } catch {
    return fallback;
  }
}

function saveJSON(key: string, value: unknown) {
  try {
    localStorage.setItem(key, JSON.stringify(value));
  } catch {
    // Not remembered; the choice still applies now.
  }
}

/** What the clients already have of a hit: the very torrent, or the same release from another tracker. */
interface Have {
  exact: Torrent[];
  release: Torrent[];
}

/** Searches the trackers set up in Prowlarr, and sends a hit to a client. */
export function SearchPage({ instances, torrents, colorOf, onBack, onOpenSettings, onAdded }: Props) {
  const inputRef = useRef<HTMLInputElement>(null);
  const [indexers, setIndexers] = useState<SearchIndexer[] | null>(null);
  const [setupError, setSetupError] = useState<string | null>(null);
  const [hidden, setHidden] = useState<number[]>(() => loadJSON(HIDDEN_INDEXERS_KEY, []));
  const [category, setCategory] = useState(0);
  const [query, setQuery] = useState('');
  const [searching, setSearching] = useState(false);
  const [hits, setHits] = useState<SearchHit[] | null>(null);
  const [took, setTook] = useState(0);
  const [error, setError] = useState<string | null>(null);
  const [sort, setSort] = useState<{ key: SortKey; dir: 1 | -1 }>({ key: 'seeders', dir: -1 });
  const [grabbing, setGrabbing] = useState<SearchHit | null>(null);
  const [sent, setSent] = useState<Set<string>>(new Set());

  useEffect(() => {
    api
      .searchIndexers()
      .then((list) => setIndexers(list.filter((i) => i.enabled)))
      .catch((e: Error) => setSetupError(e.message));
    inputRef.current?.focus();
  }, []);

  const chosen = (indexers ?? []).filter((i) => !hidden.includes(i.id));
  const toggleIndexer = (id: number) => {
    const next = hidden.includes(id) ? hidden.filter((h) => h !== id) : [...hidden, id];
    setHidden(next);
    saveJSON(HIDDEN_INDEXERS_KEY, next);
  };

  const run = async (e?: React.FormEvent) => {
    e?.preventDefault();
    if (!query.trim() || chosen.length === 0) return;
    setSearching(true);
    setError(null);
    try {
      const res = await api.search(
        query,
        chosen.map((i) => i.id),
        CATEGORIES[category].ids,
      );
      setHits(res.hits);
      setTook(res.tookMs);
      setSent(new Set());
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setSearching(false);
    }
  };

  // Look each hit up among the torrents the clients have.
  const haveOf = useMemo(() => {
    const byHash = new Map<string, Torrent[]>();
    const byName = new Map<string, Torrent[]>();
    for (const t of torrents) {
      for (const h of [t.hash, t.altHash]) {
        if (h) byHash.set(h, [...(byHash.get(h) ?? []), t]);
      }
      const n = looseName(t.name);
      byName.set(n, [...(byName.get(n) ?? []), t]);
    }
    return (hit: SearchHit): Have => {
      const exact = hit.infoHash ? (byHash.get(hit.infoHash) ?? []) : [];
      const exactKeys = new Set(exact.map(torrentKey));
      const release = (byName.get(looseName(hit.title)) ?? []).filter(
        (t) => !exactKeys.has(torrentKey(t)) && sameSize(t.size, hit.size),
      );
      return { exact, release };
    };
  }, [torrents]);

  const sorted = useMemo(() => {
    if (!hits) return [];
    const value = (h: SearchHit): number | string => {
      switch (sort.key) {
        case 'title':
          return h.title.toLowerCase();
        case 'indexer':
          return h.indexer.toLowerCase();
        case 'age':
          return -(Date.parse(h.publishDate ?? '') || 0);
        default:
          return h[sort.key] ?? -1;
      }
    };
    return [...hits].sort((a, b) => {
      const va = value(a);
      const vb = value(b);
      const c = typeof va === 'string' ? va.localeCompare(String(vb), 'nb') : va - (vb as number);
      return c * sort.dir || a.title.localeCompare(b.title);
    });
  }, [hits, sort]);

  const header = (key: SortKey, label: string, numeric = false) => {
    const active = sort.key === key;
    return (
      <th scope="col" className={numeric ? 'is-numeric' : undefined} aria-sort={active ? (sort.dir === 1 ? 'ascending' : 'descending') : 'none'}>
        <button
          type="button"
          className="th-button"
          onClick={() =>
            setSort((s) =>
              s.key === key ? { key, dir: s.dir === 1 ? -1 : 1 } : { key, dir: key === 'title' || key === 'indexer' ? 1 : -1 },
            )
          }
        >
          {label}
          <span className="th-arrow" aria-hidden="true">
            {active ? (sort.dir === 1 ? '▲' : '▼') : ''}
          </span>
        </button>
      </th>
    );
  };

  const nameOf = (id: string) => instances.find((i) => i.id === id)?.name ?? id;

  return (
    <div className="settings search-page">
      <header className="settings-head">
        <button type="button" className="link settings-back" onClick={onBack}>
          Tilbake til torrentene
        </button>
        <h1 className="settings-title">Søk på trackerne</h1>
      </header>

      {setupError ? (
        <div className="status-bad">
          <p>{setupError}</p>
          <p>
            <button type="button" className="link" onClick={onOpenSettings}>
              Sett opp Prowlarr under Innstillinger
            </button>
          </p>
        </div>
      ) : (
        <>
          <form className="search-form" onSubmit={run} role="search">
            <input
              ref={inputRef}
              type="search"
              className="search-query"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="For eksempel Farmen S22"
              aria-label="Søk etter"
            />
            <select
              className="search-category"
              value={category}
              onChange={(e) => setCategory(Number(e.target.value))}
              aria-label="Kategori"
            >
              {CATEGORIES.map((c, i) => (
                <option key={c.label} value={i}>
                  {c.label}
                </option>
              ))}
            </select>
            <button type="submit" className="btn btn-primary" disabled={searching || !query.trim() || chosen.length === 0}>
              {searching ? 'Søker …' : 'Søk'}
            </button>
          </form>

          {indexers && (
            <fieldset className="search-indexers">
              <legend className="field-label">Trackere</legend>
              {indexers.length === 0 && <p className="hint">Prowlarr har ingen aktive torrent-indeksere.</p>}
              {indexers.map((i) => (
                <label key={i.id} className="search-indexer">
                  <input type="checkbox" checked={!hidden.includes(i.id)} onChange={() => toggleIndexer(i.id)} />
                  <span>{i.name}</span>
                </label>
              ))}
            </fieldset>
          )}

          <div aria-live="polite">
            {searching && <p className="hint">Søker i {plural(chosen.length, 'tracker', 'trackere')} …</p>}
            {error && <p className="status-bad">{error}</p>}
            {hits && !searching && (
              <p className="hint search-summary">
                {hits.length === 0
                  ? 'Ingen treff.'
                  : `${plural(hits.length, 'treff', 'treff')} på ${(took / 1000).toLocaleString('nb-NO', { maximumFractionDigits: 1 })} s.`}
              </p>
            )}
          </div>

          {sorted.length > 0 && (
            <div className="search-results-wrap">
              <table className="search-results">
                <thead>
                  <tr>
                    {header('title', 'Tittel')}
                    {header('indexer', 'Tracker')}
                    {header('size', 'Størrelse', true)}
                    {header('seeders', 'Seedere', true)}
                    {header('leechers', 'Leechere', true)}
                    {header('grabs', 'Fullført', true)}
                    {header('age', 'Alder', true)}
                    <th scope="col">
                      <span className="sr-only">Last ned</span>
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {sorted.map((h) => {
                    const have = haveOf(h);
                    return (
                      <tr key={h.id}>
                        <td className="search-title">
                          <span className="search-title-text">
                            {h.infoUrl ? (
                              <a href={h.infoUrl} target="_blank" rel="noreferrer">
                                {h.title}
                              </a>
                            ) : (
                              h.title
                            )}
                          </span>
                          <span className="search-badges">
                            {h.freeleech && <span className="badge badge-free">Freeleech</span>}
                            {have.exact.length > 0 && (
                              <span className="badge badge-have" title={`Ligger i ${have.exact.map((t) => nameOf(t.instanceId)).join(', ')}`}>
                                Har den
                              </span>
                            )}
                            {have.exact.length === 0 && have.release.length > 0 && (
                              <span
                                className="badge badge-release"
                                title={`Samme utgivelse fra ${[...new Set(have.release.map((t) => t.tracker || 'ukjent tracker'))].join(', ')}`}
                              >
                                Har utgivelsen
                              </span>
                            )}
                            {sent.has(h.id) && <span className="badge badge-have">Sendt</span>}
                          </span>
                        </td>
                        <td>{h.indexer}</td>
                        <td className="is-numeric">{bytes(h.size)}</td>
                        <td className="is-numeric">{h.seeders ?? '–'}</td>
                        <td className="is-numeric">{h.leechers ?? '–'}</td>
                        <td className="is-numeric">{h.grabs ?? '–'}</td>
                        <td className="is-numeric">{age(h.publishDate)}</td>
                        <td>
                          <button type="button" className="btn btn-small" onClick={() => setGrabbing(h)}>
                            Last ned
                          </button>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          )}
        </>
      )}

      {grabbing && (
        <GrabDialog
          hit={grabbing}
          have={haveOf(grabbing)}
          instances={instances}
          colorOf={colorOf}
          onClose={() => setGrabbing(null)}
          onDone={(client) => {
            setSent((prev) => new Set(prev).add(grabbing.id));
            setGrabbing(null);
            onAdded(`Lagt til i ${client}`);
          }}
        />
      )}
    </div>
  );
}

const plural = (n: number, one: string, many: string) => `${count(n)} ${n === 1 ? one : many}`;

interface GrabProps {
  hit: SearchHit;
  have: Have;
  instances: InstanceStatus[];
  colorOf: (instanceId: string) => string;
  onClose: () => void;
  onDone: (client: string) => void;
}

function GrabDialog({ hit, have, instances, colorOf, onClose, onDone }: GrabProps) {
  const ref = useRef<HTMLDivElement>(null);
  const last = loadJSON<{ instanceId?: string; label?: string }>(LAST_GRAB_KEY, {});
  const online = instances.filter((i) => i.online);
  // Clients that already have this very torrent cannot take it again.
  const hasIt = new Set(have.exact.map((t) => t.instanceId));
  const open = online.filter((i) => !hasIt.has(i.id));
  // A cross-seed: the release is already in a client, so add it there, next to its files.
  const twin = have.release.find((t) => !hasIt.has(t.instanceId));
  const [instanceId, setInstanceId] = useState(
    twin?.instanceId ?? (open.some((i) => i.id === last.instanceId) ? last.instanceId! : (open[0]?.id ?? '')),
  );
  const [savePath, setSavePath] = useState(twin?.savePath ?? '');
  const [label, setLabel] = useState(twin ? twin.label : (last.label ?? ''));
  const [start, setStart] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [defaultPath, setDefaultPath] = useState('');

  useEffect(() => {
    if (!instanceId) return;
    let stale = false;
    api
      .savePath(instanceId)
      .then((r) => !stale && setDefaultPath(r.path))
      .catch(() => !stale && setDefaultPath(''));
    return () => {
      stale = true;
    };
  }, [instanceId]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const target = e.target as Node | null;
      const aimedHere = target === document.body || (target !== null && ref.current?.contains(target));
      if (e.key === 'Escape' && e.isTrusted && !e.defaultPrevented && aimedHere) onClose();
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [onClose]);

  const twinHere = have.release.find((t) => t.instanceId === instanceId);
  const nextToFiles = twinHere && savePath.replace(/\/+$/, '') === twinHere.savePath.replace(/\/+$/, '');
  const target = instances.find((i) => i.id === instanceId);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const res = await api.grab({ id: hit.id, instanceId, savePath: savePath.trim(), label: label.trim(), paused: !start });
      saveJSON(LAST_GRAB_KEY, { instanceId, label: label.trim() });
      onDone(res.client);
    } catch (err) {
      setError((err as Error).message);
      setBusy(false);
    }
  };

  return (
    <div className="modal-layer">
      <div ref={ref} className="dialog" role="dialog" aria-modal="true" aria-labelledby="grab-title">
        <form className="dialog-body" onSubmit={submit}>
          <div>
            <h2 className="dialog-title" id="grab-title">
              Last ned
            </h2>
            <p className="hint grab-what">
              {hit.title}, fra {hit.indexer}, {bytes(hit.size)}
            </p>
          </div>

          {have.exact.length > 0 && (
            <p className="status-bad">
              Denne torrenten ligger allerede i {have.exact.map((t) => instances.find((i) => i.id === t.instanceId)?.name).join(', ')}.
            </p>
          )}

          <fieldset className="target-picker">
            <legend className="field-label">Til klient</legend>
            <div className="target-options">
              {online.map((inst) => (
                <label key={inst.id} className="target-option">
                  <input
                    type="radio"
                    name="grab-target"
                    checked={instanceId === inst.id}
                    disabled={hasIt.has(inst.id)}
                    onChange={() => {
                      setInstanceId(inst.id);
                      const t = have.release.find((r) => r.instanceId === inst.id);
                      setSavePath(t?.savePath ?? '');
                    }}
                  />
                  <span className="inst-dot" style={{ background: colorOf(inst.id) }} aria-hidden="true" />
                  <span className="target-name">{inst.name}</span>
                  {hasIt.has(inst.id) ? (
                    <span className="target-sub">Har den</span>
                  ) : (
                    have.release.some((r) => r.instanceId === inst.id) && (
                      <span className="target-sub">Har utgivelsen</span>
                    )
                  )}
                </label>
              ))}
            </div>
          </fieldset>

          <label className="field">
            <span className="field-label">Mappe, slik {target?.name ?? 'klienten'} ser den</span>
            <input
              type="text"
              value={savePath}
              onChange={(e) => setSavePath(e.target.value)}
              placeholder={defaultPath ? `${defaultPath} (klientens standard)` : 'Klientens standard'}
              spellCheck={false}
            />
            <span className="hint">
              {nextToFiles
                ? 'Filene ligger allerede her fra en annen tracker. Klienten sjekker dem og seeder uten å laste ned på nytt.'
                : 'Tom: klienten velger selv, ut fra kategorien eller standardmappen.'}
            </span>
          </label>

          <label className="field">
            <span className="field-label">Kategori eller etikett</span>
            <input type="text" value={label} onChange={(e) => setLabel(e.target.value)} placeholder="Ingen" spellCheck={false} />
          </label>

          <label className="check">
            <input type="checkbox" checked={start} onChange={(e) => setStart(e.target.checked)} />
            <span>Start med en gang</span>
          </label>

          {error && <p className="status-bad">{error}</p>}

          <div className="dialog-actions">
            <span className="dialog-spacer" />
            <button type="button" className="btn" onClick={onClose}>
              Avbryt
            </button>
            <button type="submit" className="btn btn-primary" disabled={busy || !instanceId}>
              {busy ? 'Sender …' : `Last ned til ${target?.name ?? ''}`}
            </button>
          </div>
        </form>
        <button type="button" className="dialog-close" onClick={onClose} aria-label="Lukk">
          ×
        </button>
      </div>
    </div>
  );
}
