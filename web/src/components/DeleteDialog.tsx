import { useEffect, useMemo, useRef, useState } from 'react';
import { api } from '../api';
import type { InstanceStatus, Torrent } from '../types';
import { bytes, count } from '../format';
import { torrentKey } from './TorrentTable';

interface Props {
  /** The torrents to remove. */
  torrents: Torrent[];
  /** Every torrent in every client, to find ones sharing the same files. */
  allTorrents: Torrent[];
  instances: InstanceStatus[];
  colorOf: (instanceId: string) => string;
  onClose: () => void;
  onDone: (result: { removed: number; deleteData: boolean; failed: Record<string, string> }) => void;
}

const plural = (n: number, one: string, many: string) => `${count(n)} ${n === 1 ? one : many}`;

/** Where a torrent's files are: its save folder plus its name. */
const contentPath = (t: Torrent) => `${t.savePath.replace(/\/+$/, '')}/${t.name}`;

const SHOW_SHARED = 5;

export function DeleteDialog({ torrents, allTorrents, instances, colorOf, onClose, onDone }: Props) {
  const ref = useRef<HTMLDivElement>(null);
  const cancelRef = useRef<HTMLButtonElement>(null);
  const [confirmData, setConfirmData] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const target = e.target as Node | null;
      const aimedHere = target === document.body || (target !== null && ref.current?.contains(target));
      if (e.key === 'Escape' && e.isTrusted && !e.defaultPrevented && aimedHere) onClose();
    };
    document.addEventListener('keydown', onKey);
    // Start on the harmless choice.
    cancelRef.current?.focus();
    return () => document.removeEventListener('keydown', onKey);
  }, [onClose]);

  const nameOf = (id: string) => instances.find((i) => i.id === id)?.name ?? id;
  const size = torrents.reduce((n, t) => n + t.size, 0);
  const perClient = useMemo(() => {
    const m = new Map<string, number>();
    for (const t of torrents) m.set(t.instanceId, (m.get(t.instanceId) ?? 0) + 1);
    return [...m];
  }, [torrents]);

  // Torrents that are not being removed but use the same files: the same
  // torrent in another client, or a cross-seed in the same folder.
  const shared = useMemo(() => {
    const selected = new Set(torrents.map(torrentKey));
    const byPath = new Map<string, Torrent[]>();
    const byHash = new Map<string, Torrent[]>();
    for (const t of allTorrents) {
      if (selected.has(torrentKey(t))) continue;
      const p = contentPath(t);
      byPath.set(p, [...(byPath.get(p) ?? []), t]);
      byHash.set(t.hash, [...(byHash.get(t.hash) ?? []), t]);
    }
    const out: { torrent: Torrent; other: Torrent }[] = [];
    const seen = new Set<string>();
    for (const t of torrents) {
      for (const other of [...(byPath.get(contentPath(t)) ?? []), ...(byHash.get(t.hash) ?? [])]) {
        const key = `${torrentKey(t)}>${torrentKey(other)}`;
        if (!seen.has(key)) {
          seen.add(key);
          out.push({ torrent: t, other });
        }
      }
    }
    return out;
  }, [torrents, allTorrents]);

  const run = async (deleteData: boolean) => {
    setBusy(true);
    setError(null);
    try {
      const { failed } = await api.remove(
        torrents.map((t) => ({ instanceId: t.instanceId, hash: t.hash })),
        deleteData,
      );
      onDone({ removed: torrents.length, deleteData, failed });
    } catch (e) {
      setError((e as Error).message);
      setBusy(false);
    }
  };

  const title =
    torrents.length === 1 ? 'Slett torrenten' : `Slett ${plural(torrents.length, 'torrent', 'torrenter')}`;

  return (
    <div className="modal-layer">
      <div ref={ref} className="dialog" role="alertdialog" aria-modal="true" aria-labelledby="delete-title">
        <div className="dialog-body">
          <div>
            <h2 className="dialog-title" id="delete-title">
              {title}
            </h2>
            <p className="hint">
              {torrents.length === 1 ? torrents[0].name : `${bytes(size)} til sammen`}
            </p>
          </div>

          {perClient.length > 1 || torrents.length > 1 ? (
            <ul className="delete-clients">
              {perClient.map(([id, n]) => (
                <li key={id}>
                  <span className="inst-dot" style={{ background: colorOf(id) }} aria-hidden="true" />
                  {nameOf(id)}
                  <span className="delete-count">{plural(n, 'torrent', 'torrenter')}</span>
                </li>
              ))}
            </ul>
          ) : (
            <p>
              Fra {nameOf(torrents[0].instanceId)}, {bytes(size)}.
            </p>
          )}

          {shared.length > 0 && (
            <div className="status-bad">
              <p>
                <strong>
                  {shared.length === 1
                    ? 'En annen torrent ser ut til å bruke de samme filene:'
                    : `${count(shared.length)} andre torrenter ser ut til å bruke de samme filene:`}
                </strong>
              </p>
              <ul className="delete-shared">
                {shared.slice(0, SHOW_SHARED).map(({ torrent, other }) => (
                  <li key={`${torrentKey(torrent)}>${torrentKey(other)}`}>
                    <span className="ellipsis" title={other.name}>
                      {other.name}
                    </span>
                    <span className="delete-count">i {nameOf(other.instanceId)}</span>
                  </li>
                ))}
                {shared.length > SHOW_SHARED && <li>og {count(shared.length - SHOW_SHARED)} til</li>}
              </ul>
              <p>Sletter du filene, slutter de å virke. «Fjern bare torrentene» lar filene ligge.</p>
            </div>
          )}

          <p className="hint">
            «Fjern bare torrentene» tar {torrents.length === 1 ? 'den' : 'dem'} ut av klienten og lar filene ligge på
            disken. «Slett torrenter og filer» sletter også filene, og kan ikke angres.
          </p>

          {error && <p className="status-bad">{error}</p>}

          <div className="dialog-actions">
            <button ref={cancelRef} type="button" className="btn" onClick={onClose} disabled={busy}>
              Avbryt
            </button>
            <span className="dialog-spacer" />
            <button type="button" className="btn" onClick={() => run(false)} disabled={busy}>
              Fjern bare torrentene
            </button>
            {confirmData ? (
              <button type="button" className="btn btn-danger" onClick={() => run(true)} disabled={busy}>
                Bekreft: slett {bytes(size)}
              </button>
            ) : (
              <button type="button" className="btn btn-danger" onClick={() => setConfirmData(true)} disabled={busy}>
                Slett torrenter og filer
              </button>
            )}
          </div>
        </div>
        <button type="button" className="dialog-close" onClick={onClose} aria-label="Lukk">
          ×
        </button>
      </div>
    </div>
  );
}
