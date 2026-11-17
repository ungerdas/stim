import { useEffect, useRef, useState } from 'react';
import { api } from '../api';
import type { Details, InstanceStatus, Torrent } from '../types';
import { bytes, count, date, percent, rate, ratio } from '../format';
import { stateLabel } from '../text';

interface Props {
  torrent: Torrent;
  instance: InstanceStatus | undefined;
  color: string;
  onClose: () => void;
}

type Tab = 'overview' | 'files' | 'trackers' | 'peers';

const POLL_MS = 3000;

const TRACKER_STATUS: Record<string, string> = {
  working: 'Virker',
  updating: 'Melder seg',
  error: 'Feiler',
  waiting: 'Venter',
  disabled: 'Av',
};

/** The details of one torrent, beside the list. */
export function DetailsPanel({ torrent: t, instance, color, onClose }: Props) {
  const ref = useRef<HTMLElement>(null);
  const [tab, setTab] = useState<Tab>('overview');
  const [details, setDetails] = useState<Details | null>(null);
  const [error, setError] = useState<string | null>(null);

  // Fetch now and then every few seconds, for the torrent shown.
  useEffect(() => {
    let stopped = false;
    let timer: number | undefined;
    setDetails(null);
    setError(null);
    const load = async () => {
      if (!document.hidden) {
        try {
          const d = await api.details(t.instanceId, t.hash);
          if (!stopped) {
            setDetails(d);
            setError(null);
          }
        } catch (e) {
          if (!stopped) setError((e as Error).message);
        }
      }
      if (!stopped) timer = window.setTimeout(load, POLL_MS);
    };
    load();
    return () => {
      stopped = true;
      window.clearTimeout(timer);
    };
  }, [t.instanceId, t.hash]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const target = e.target as Node | null;
      const aimedHere = target === document.body || (target !== null && ref.current?.contains(target));
      if (e.key === 'Escape' && e.isTrusted && !e.defaultPrevented && aimedHere) onClose();
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [onClose]);

  const tabs: { id: Tab; label: string; n?: number }[] = [
    { id: 'overview', label: 'Oversikt' },
    { id: 'files', label: 'Filer', n: details?.files.length },
    { id: 'trackers', label: 'Trackere', n: details?.trackers.length },
    { id: 'peers', label: 'Peers', n: details?.peers.length },
  ];

  return (
    <aside ref={ref} className="details" aria-labelledby="details-title">
      <header className="details-head">
        <h2 id="details-title" className="details-title">
          {t.name}
        </h2>
        <p className="details-sub">
          <span className="inst-dot" style={{ background: color }} aria-hidden="true" />
          <span>{instance?.name ?? ''}</span>
          <span className={`state-text state-${t.state}`}>{stateLabel[t.state]}</span>
          {t.trackerStatus && (
            <span className={`tracker-${t.trackerStatus}`}>
              {t.trackerStatus === 'unregistered' ? 'Avregistrert' : 'Trackerfeil'}
            </span>
          )}
        </p>
        <button type="button" className="dialog-close" onClick={onClose} aria-label="Lukk detaljene">
          ×
        </button>
        <div className="details-tabs" role="tablist" aria-label="Detaljer">
          {tabs.map((x) => (
            <button
              key={x.id}
              type="button"
              role="tab"
              id={`details-tab-${x.id}`}
              aria-selected={tab === x.id}
              aria-controls="details-panel"
              className="details-tab"
              onClick={() => setTab(x.id)}
            >
              {x.label}
              {x.n !== undefined && <span className="details-tab-count">{count(x.n)}</span>}
            </button>
          ))}
        </div>
      </header>

      <div className="details-body" id="details-panel" role="tabpanel" aria-labelledby={`details-tab-${tab}`}>
        {error && <p className="status-bad">Fikk ikke hentet detaljene: {error}</p>}
        {tab === 'overview' && <Overview t={t} d={details} />}
        {tab !== 'overview' && !details && !error && <p className="hint">Henter …</p>}
        {tab === 'files' && details && <Files d={details} />}
        {tab === 'trackers' && details && <Trackers d={details} />}
        {tab === 'peers' && details && <Peers d={details} />}
      </div>
    </aside>
  );
}

function Overview({ t, d }: { t: Torrent; d: Details | null }) {
  const rows: [string, React.ReactNode][] = [
    ['Status', `${stateLabel[t.state]}${t.rawState ? ` (${t.rawState})` : ''}`],
    ['Fremdrift', percent(t.progress)],
    ['Størrelse', bytes(t.size)],
    ['Lastet ned', bytes(t.downloaded)],
    ['Lastet opp', bytes(t.uploaded)],
    ['Ratio', ratio(t.ratio)],
    ['Fart', [t.downloadRate > 0 && `↓ ${rate(t.downloadRate)}`, t.uploadRate > 0 && `↑ ${rate(t.uploadRate)}`].filter(Boolean).join('  ') || 'Ingen nå'],
    ['Lagret i', <span className="details-path">{t.contentPath || `${t.savePath.replace(/\/+$/, '')}/${t.name}`}</span>],
    ['Tracker', t.tracker || 'Ingen'],
  ];
  if (t.trackerMessage) rows.push(['Trackeren sier', <span className={`tracker-${t.trackerStatus}`}>{t.trackerMessage}</span>]);
  if (t.message && t.message !== 'OK') rows.push(['Melding', t.message]);
  if (t.label) rows.push(['Etikett', t.label]);
  if (t.tags.length > 0) rows.push(['Tagger', t.tags.join(', ')]);
  rows.push(['Lagt til', date(t.addedAt) || '–']);
  if (t.completedAt) rows.push(['Fullført', date(t.completedAt)]);
  if (d?.private !== undefined) rows.push(['Privat', d.private ? 'Ja' : 'Nei']);
  if (d?.pieces) rows.push(['Biter', `${count(d.pieces)} × ${bytes(d.pieceSize ?? 0)}`]);
  if (d?.createdBy) rows.push(['Laget med', d.createdBy]);
  if (d?.createdAt) rows.push(['Laget', date(d.createdAt)]);
  if (d?.comment) rows.push(['Kommentar', d.comment]);
  rows.push(['Info-hash', <span className="details-path">{t.hash}{t.altHash ? ` (også ${t.altHash})` : ''}</span>]);
  return (
    <dl className="details-facts">
      {rows.map(([k, v]) => (
        <div key={k} className="details-fact">
          <dt>{k}</dt>
          <dd>{v}</dd>
        </div>
      ))}
    </dl>
  );
}

function Files({ d }: { d: Details }) {
  if (d.files.length === 0) return <p className="hint">Klienten oppgir ingen filer ennå.</p>;
  return (
    <ul className="details-list">
      {d.files.map((f) => (
        <li key={f.path} className={`details-file${f.wanted ? '' : ' is-skipped'}`}>
          <span className="details-file-path">{f.path}</span>
          <span className="details-file-size">{bytes(f.size)}</span>
          <span className="details-file-progress">
            {f.wanted ? (
              <>
                <span className="bar" aria-hidden="true">
                  <span className="bar-fill" style={{ width: `${Math.min(1, f.progress) * 100}%` }} />
                </span>
                <span>{percent(f.progress)}</span>
              </>
            ) : (
              'Hoppes over'
            )}
          </span>
        </li>
      ))}
    </ul>
  );
}

function Trackers({ d }: { d: Details }) {
  if (d.trackers.length === 0) return <p className="hint">Ingen trackere.</p>;
  const n = (v: number) => (v >= 0 ? count(v) : '–');
  return (
    <ul className="details-list">
      {d.trackers.map((tr, i) => (
        <li key={`${tr.url}-${i}`} className="details-tracker">
          <span className="details-path">{tr.url}</span>
          <span className={`details-tracker-status is-${tr.status}`}>{TRACKER_STATUS[tr.status] ?? tr.status}</span>
          {tr.message && <span className="details-tracker-msg">{tr.message}</span>}
          <span className="details-tracker-counts">
            Seedere {n(tr.seeds)}, leechere {n(tr.leeches)}, fullført {n(tr.downloaded)}
          </span>
        </li>
      ))}
    </ul>
  );
}

function Peers({ d }: { d: Details }) {
  if (d.peers.length === 0) return <p className="hint">Ingen tilkoblede peers akkurat nå.</p>;
  return (
    <table className="details-peers">
      <thead>
        <tr>
          <th scope="col">Adresse</th>
          <th scope="col">Klient</th>
          <th scope="col" className="is-numeric">Har</th>
          <th scope="col" className="is-numeric">Ned</th>
          <th scope="col" className="is-numeric">Opp</th>
        </tr>
      </thead>
      <tbody>
        {d.peers.map((p) => (
          <tr key={p.address}>
            <td>
              {p.address}
              {p.country && <span className="details-peer-country"> {p.country}</span>}
            </td>
            <td>{p.client || '–'}</td>
            <td className="is-numeric">{percent(p.progress)}</td>
            <td className="is-numeric">{rate(p.downRate)}</td>
            <td className="is-numeric">{rate(p.upRate)}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}
