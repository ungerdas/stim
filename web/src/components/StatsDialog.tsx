import { useEffect, useMemo, useRef } from 'react';
import type { InstanceStatus, KindInfo, Torrent } from '../types';
import { bytes, count, ratio } from '../format';
import { trackerLabel } from './Sidebar';

interface Props {
  instances: InstanceStatus[];
  torrents: Torrent[];
  kinds: KindInfo[];
  colorOf: (instanceId: string) => string;
  onClose: () => void;
}

interface Group {
  key: string;
  torrents: number;
  size: number;
  uploaded: number;
  downloaded: number;
}

function group(torrents: Torrent[], keyOf: (t: Torrent) => string): Group[] {
  const map = new Map<string, Group>();
  for (const t of torrents) {
    const key = keyOf(t);
    let g = map.get(key);
    if (!g) {
      g = { key, torrents: 0, size: 0, uploaded: 0, downloaded: 0 };
      map.set(key, g);
    }
    g.torrents++;
    g.size += t.size;
    g.uploaded += t.uploaded;
    g.downloaded += t.downloaded;
  }
  return [...map.values()].sort((a, b) => b.uploaded - a.uploaded || a.key.localeCompare(b.key));
}

const sum = (groups: Group[]): Group =>
  groups.reduce(
    (acc, g) => ({
      ...acc,
      torrents: acc.torrents + g.torrents,
      size: acc.size + g.size,
      uploaded: acc.uploaded + g.uploaded,
      downloaded: acc.downloaded + g.downloaded,
    }),
    { key: '', torrents: 0, size: 0, uploaded: 0, downloaded: 0 },
  );

const ratioOf = (up: number, down: number) => (down > 0 ? ratio(up / down) : '–');
const known = (n: number | undefined) => (n === undefined || n < 0 ? '–' : bytes(n));

/** Uploaded and downloaded amounts per tracker and per client. */
export function StatsDialog({ instances, torrents, kinds, colorOf, onClose }: Props) {
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const target = e.target as Node | null;
      const aimedHere = target === document.body || (target !== null && ref.current?.contains(target));
      if (e.key === 'Escape' && e.isTrusted && !e.defaultPrevented && aimedHere) onClose();
    };
    document.addEventListener('keydown', onKey);
    ref.current?.querySelector<HTMLButtonElement>('.dialog-close')?.focus();
    return () => document.removeEventListener('keydown', onKey);
  }, [onClose]);

  const byTracker = useMemo(() => group(torrents, (t) => t.tracker), [torrents]);
  const byClient = useMemo(() => {
    const groups = new Map(group(torrents, (t) => t.instanceId).map((g) => [g.key, g]));
    // Every client gets a row, also one without torrents.
    return instances.map(
      (inst) => groups.get(inst.id) ?? { key: inst.id, torrents: 0, size: 0, uploaded: 0, downloaded: 0 },
    );
  }, [torrents, instances]);
  const total = sum(byTracker);

  const instanceOf = (id: string) => instances.find((i) => i.id === id);
  const kindLabel = (kind: string) => kinds.find((k) => k.kind === kind)?.label ?? kind;
  const delugeWithoutTotals = instances.some((i) => i.kind === 'deluge' && (i.totals?.allTimeUpload ?? -1) < 0);

  return (
    <div className="modal-layer">
      <div ref={ref} className="dialog dialog-wide" role="dialog" aria-modal="true" aria-labelledby="stats-title">
        <div className="dialog-body">
          <div>
            <h2 className="dialog-title" id="stats-title">
              Statistikk
            </h2>
            <p className="hint">
              {count(total.torrents)} torrenter i {count(instances.length)}{' '}
              {instances.length === 1 ? 'klient' : 'klienter'}
            </p>
          </div>

          <dl className="stats-hero">
            <div>
              <dt>Lastet opp</dt>
              <dd>{bytes(total.uploaded)}</dd>
            </div>
            <div>
              <dt>Lastet ned</dt>
              <dd>{bytes(total.downloaded)}</dd>
            </div>
            <div>
              <dt>Ratio</dt>
              <dd>{ratioOf(total.uploaded, total.downloaded)}</dd>
            </div>
          </dl>

          <section aria-labelledby="stats-trackers">
            <h3 className="stats-heading" id="stats-trackers">
              Per tracker
            </h3>
            <div className="stats-scroll">
              <table className="stats-table">
                <thead>
                  <tr>
                    <th scope="col">Tracker</th>
                    <th scope="col">Torrenter</th>
                    <th scope="col">Størrelse</th>
                    <th scope="col">Lastet opp</th>
                    <th scope="col">Lastet ned</th>
                    <th scope="col">Ratio</th>
                  </tr>
                </thead>
                <tbody>
                  {byTracker.map((g) => (
                    <tr key={g.key}>
                      <th scope="row">{trackerLabel(g.key)}</th>
                      <td>{count(g.torrents)}</td>
                      <td>{bytes(g.size)}</td>
                      <td className="stats-strong">{bytes(g.uploaded)}</td>
                      <td>{bytes(g.downloaded)}</td>
                      <td>{ratioOf(g.uploaded, g.downloaded)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </section>

          <section aria-labelledby="stats-clients">
            <h3 className="stats-heading" id="stats-clients">
              Per klient
            </h3>
            <div className="stats-scroll">
              <table className="stats-table">
                <thead>
                  <tr>
                    <th scope="col">Klient</th>
                    <th scope="col">Torrenter</th>
                    <th scope="col">Lastet opp</th>
                    <th scope="col" title="Klientens egen sum, også for fjernede torrenter">
                      Totalt i klienten
                    </th>
                    <th scope="col" title="Siden klienten sist startet">
                      Siden omstart
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {byClient.map((g) => {
                    const inst = instanceOf(g.key);
                    return (
                      <tr key={g.key}>
                        <th scope="row" title={inst && kindLabel(inst.kind)}>
                          <span className="stats-client">
                            <span className="inst-dot" style={{ background: colorOf(g.key) }} aria-hidden="true" />
                            {inst?.name}
                          </span>
                        </th>
                        <td>{count(g.torrents)}</td>
                        <td className="stats-strong">{bytes(g.uploaded)}</td>
                        <td>{known(inst?.totals?.allTimeUpload)}</td>
                        <td>{known(inst?.totals?.sessionUpload)}</td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          </section>

          <div className="hint stats-notes">
            <p>
              «Lastet opp» er summen for torrentene som ligger i klientene nå. Torrenter du har fjernet er ikke med,
              og en torrent som flyttes til en annen klient begynner på 0 der.
            </p>
            <p>
              «Totalt i klienten» er klientens egen sum over alle tider, også for fjernede torrenter, og inkluderer
              protokolltrafikk.
              {delugeWithoutTotals &&
                ' Deluge fører bare denne summen med Stats-pluginen slått på (Innstillinger → Plugins), og teller da fra den ble slått på.'}
            </p>
          </div>
        </div>
        <button type="button" className="dialog-close" onClick={onClose} aria-label="Lukk">
          ×
        </button>
      </div>
    </div>
  );
}
