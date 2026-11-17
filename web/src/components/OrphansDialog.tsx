import { useEffect, useMemo, useRef, useState } from 'react';
import { api } from '../api';
import type { OrphanScan } from '../types';
import { bytes, count, date } from '../format';

interface Props {
  server: string;
  onClose: () => void;
  onEditClients: () => void;
}

type Phase = 'scanning' | 'list' | 'confirm' | 'deleting' | 'done' | 'error';

const plural = (n: number, one: string, many: string) => `${count(n)} ${n === 1 ? one : many}`;

/** Finds and removes files in a server's download folders that no torrent uses. */
export function OrphansDialog({ server, onClose, onEditClients }: Props) {
  const ref = useRef<HTMLDivElement>(null);
  const [phase, setPhase] = useState<Phase>('scanning');
  const [scan, setScan] = useState<OrphanScan | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [picked, setPicked] = useState<Set<string>>(new Set());
  const [result, setResult] = useState<{ removed: string[]; failed: Record<string, string> } | null>(null);

  const run = async () => {
    setPhase('scanning');
    setError(null);
    setPicked(new Set());
    try {
      setScan(await api.scanOrphans(server));
      setPhase('list');
    } catch (e) {
      setError((e as Error).message);
      setPhase('error');
    }
  };

  useEffect(() => {
    run();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [server]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const target = e.target as Node | null;
      const aimedHere = target === document.body || (target !== null && ref.current?.contains(target));
      if (e.key === 'Escape' && e.isTrusted && !e.defaultPrevented && aimedHere && phase !== 'deleting') onClose();
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [onClose, phase]);

  const items = useMemo(() => scan?.items ?? [], [scan]);
  const pickedSize = items.filter((i) => picked.has(i.path)).reduce((n, i) => n + i.size, 0);
  const allPicked = items.length > 0 && items.every((i) => picked.has(i.path));
  const toggle = (p: string) =>
    setPicked((prev) => {
      const next = new Set(prev);
      if (next.has(p)) next.delete(p);
      else next.add(p);
      return next;
    });

  const remove = async () => {
    setPhase('deleting');
    try {
      const res = await api.deleteOrphans(server, [...picked]);
      setResult(res);
      setPhase('done');
    } catch (e) {
      setError((e as Error).message);
      setPhase('error');
    }
  };

  const split = (p: string) => {
    const i = p.lastIndexOf('/');
    return { dir: p.slice(0, i + 1), name: p.slice(i + 1) };
  };

  return (
    <div className="modal-layer">
      <div
        ref={ref}
        className="dialog dialog-wide"
        role="dialog"
        aria-modal="true"
        aria-labelledby="orphans-title"
      >
        <div className="dialog-body">
          <div>
            <h2 className="dialog-title" id="orphans-title">
              Filer uten torrent
            </h2>
            <p className="hint">På {server}</p>
          </div>

          {phase === 'scanning' && (
            <p className="hint" aria-live="polite">
              Ser gjennom nedlastingsmappene og sammenligner med torrentene i alle klientene på serveren …
            </p>
          )}

          {phase === 'error' && (
            <div className="status-bad">
              <p>{error}</p>
              {error?.includes('SSH') && (
                <p>
                  <button type="button" className="link" onClick={onEditClients}>
                    Sett opp SSH-tilgang på en av klientene
                  </button>
                </p>
              )}
            </div>
          )}

          {scan && (phase === 'list' || phase === 'confirm') && (
            <>
              <p>
                {items.length === 0
                  ? 'Fant ingenting som ingen torrent bruker.'
                  : `Fant ${plural(items.length, 'fil eller mappe', 'filer og mapper')} som ingen torrent bruker, til sammen ${bytes(scan.total)}.`}
                {scan.recent > 0 &&
                  ` ${plural(scan.recent, 'ting', 'ting')} endret den siste timen er holdt utenfor, i tilfelle de er på vei inn i en klient.`}
              </p>
              <p className="hint">
                Torrentene i alle klientene i Stim er regnet med, og mappene til {scan.clients.join(', ')} er sett
                gjennom. Bruker en klient som ikke er lagt inn i Stim de samme mappene, kan filene dens dukke opp her. Se
                over listen før du sletter.
              </p>
              {scan.unanswered.length > 0 && (
                <p className="status-bad">
                  {scan.unanswered.join(', ')} svarte ikke, så torrentene der er ikke regnet med. Ligger de på samme
                  server, kan filene deres stå i listen.
                </p>
              )}

              {items.length > 0 && (
                <div className="orphans">
                  <label className="check orphans-all">
                    <input
                      type="checkbox"
                      checked={allPicked}
                      onChange={() => setPicked(allPicked ? new Set() : new Set(items.map((i) => i.path)))}
                      disabled={phase === 'confirm'}
                    />
                    <span>Velg alle</span>
                  </label>
                  <ul className="orphans-list">
                    {items.map((it) => {
                      const { dir, name } = split(it.path);
                      return (
                        <li key={it.path}>
                          <label className="orphan">
                            <input
                              type="checkbox"
                              checked={picked.has(it.path)}
                              onChange={() => toggle(it.path)}
                              disabled={phase === 'confirm'}
                            />
                            <span className="orphan-path" title={it.path}>
                              <span className="orphan-dir">{dir}</span>
                              <span className="orphan-name">
                                {name}
                                {it.isDir ? '/' : ''}
                              </span>
                            </span>
                            <span className="orphan-size">
                              {it.sizeIncomplete ? 'over ' : ''}
                              {bytes(it.size)}
                            </span>
                            <span className="orphan-date">{date(it.modTime)}</span>
                          </label>
                        </li>
                      );
                    })}
                  </ul>
                </div>
              )}

              <details className="advanced">
                <summary>Mapper som ble sett gjennom ({count(scan.dirs.length)})</summary>
                <ul className="orphans-dirs">
                  {scan.dirs.map((d) => (
                    <li key={d}>{d}</li>
                  ))}
                  {scan.skipped.map((d) => (
                    <li key={d} className="hint">
                      Ikke sett gjennom: {d}
                    </li>
                  ))}
                </ul>
                <p className="hint">
                  Bare det som ligger rett i disse mappene sammenlignes. En mappe som hører til en torrent, får være i fred
                  i sin helhet, også om den inneholder noe ekstra. Skjulte filer vises aldri.
                </p>
              </details>
            </>
          )}

          {phase === 'confirm' && (
            <div className="status-bad">
              <p>
                <strong>
                  Slette {plural(picked.size, 'element', 'elementer')}, {bytes(pickedSize)}?
                </strong>{' '}
                Det kan ikke angres. Stim sjekker hvert element en gang til rett før, og sletter bare det som fortsatt
                ikke brukes av noen torrent.
              </p>
            </div>
          )}

          {phase === 'deleting' && <p aria-live="polite">Sletter …</p>}

          {phase === 'done' && result && (
            <div aria-live="polite">
              <p className={Object.keys(result.failed).length > 0 ? '' : 'status-ok'}>
                Slettet {plural(result.removed.length, 'element', 'elementer')}.
              </p>
              {Object.keys(result.failed).length > 0 && (
                <div className="status-bad">
                  <p>Ble ikke slettet:</p>
                  <ul>
                    {Object.entries(result.failed).map(([p, why]) => (
                      <li key={p}>
                        {p}: {why}
                      </li>
                    ))}
                  </ul>
                </div>
              )}
            </div>
          )}

          <div className="dialog-actions">
            {(phase === 'list' || phase === 'done' || phase === 'error') && (
              <button type="button" className="btn" onClick={run}>
                Se etter på nytt
              </button>
            )}
            <span className="dialog-spacer" />
            {phase === 'confirm' ? (
              <>
                <button type="button" className="btn" onClick={() => setPhase('list')}>
                  Avbryt
                </button>
                <button type="button" className="btn btn-danger" onClick={remove}>
                  Slett for godt
                </button>
              </>
            ) : (
              <>
                <button type="button" className="btn" onClick={onClose} disabled={phase === 'deleting'}>
                  Lukk
                </button>
                {phase === 'list' && items.length > 0 && (
                  <button
                    type="button"
                    className="btn btn-danger"
                    disabled={picked.size === 0}
                    onClick={() => setPhase('confirm')}
                  >
                    {picked.size === 0 ? 'Slett valgte' : `Slett ${count(picked.size)} valgte, ${bytes(pickedSize)}`}
                  </button>
                )}
              </>
            )}
          </div>
        </div>
        <button
          type="button"
          className="dialog-close"
          onClick={onClose}
          aria-label="Lukk"
          disabled={phase === 'deleting'}
        >
          ×
        </button>
      </div>
    </div>
  );
}
