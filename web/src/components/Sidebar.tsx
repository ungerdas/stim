import type { InstanceStatus, KindInfo, State } from '../types';
import { bytes, count, ratio } from '../format';
import { instanceProblemShort, stateLabel } from '../text';

export type StateFilter = State | 'all' | 'active' | 'duplicates';

interface Props {
  instances: InstanceStatus[];
  kinds: KindInfo[];
  colorOf: (instanceId: string) => string;
  total: number;
  instanceFilter: string;
  onInstanceFilter: (id: string) => void;
  stateFilter: StateFilter;
  stateCounts: Record<StateFilter, number>;
  onStateFilter: (s: StateFilter) => void;
  onAdd: () => void;
  onEdit: (id: string) => void;
  trackers: TrackerCount[];
  hiddenTrackers: Set<string>;
  onToggleTracker: (tracker: string) => void;
  onOnlyTracker: (tracker: string) => void;
  onShowAllTrackers: () => void;
  /** Totals for the torrents in the list as it is filtered now. */
  summary: { uploaded: number; downloaded: number; filtered: boolean };
  onOpenStats: () => void;
}

export interface TrackerCount {
  /** The tracker's site, e.g. "norbits.net"; empty for torrents without one. */
  name: string;
  count: number;
}

export const trackerLabel = (name: string) => name || 'Uten tracker';

// Always shown, then any other state that currently has torrents.
const FIXED_STATES: StateFilter[] = ['all', 'active', 'downloading', 'seeding', 'paused', 'error'];
// Only shown when there is something to show (or it is the current filter).
const CONDITIONAL: StateFilter[] = ['duplicates'];
const EXTRA_STATES: StateFilter[] = ['queued', 'checking', 'moving', 'unknown'];

const buildStamp = new Intl.DateTimeFormat('nb-NO', {
  day: 'numeric',
  month: 'short',
  hour: '2-digit',
  minute: '2-digit',
}).format(new Date(__BUILD_TIME__));

function stateFilterLabel(s: StateFilter): string {
  if (s === 'all') return 'Alle';
  if (s === 'active') return 'Aktive nå';
  if (s === 'duplicates') return 'Dobbelt seedet';
  return stateLabel[s];
}

export function Sidebar(props: Props) {
  const { instances, kinds, colorOf, stateCounts } = props;
  const kindLabel = (kind: string) => kinds.find((k) => k.kind === kind)?.label ?? kind;
  const shown = (s: StateFilter) => stateCounts[s] > 0 || props.stateFilter === s;
  const states = [
    ...FIXED_STATES.slice(0, 2),
    ...CONDITIONAL.filter(shown),
    ...FIXED_STATES.slice(2),
    ...EXTRA_STATES.filter(shown),
  ];

  return (
    <aside className="sidebar">
      <div className="brand">
        <span className="brand-name">Stim</span>
        <span className="side-build">Bygget {buildStamp}</span>
      </div>

      <section className="side-summary" aria-labelledby="side-summary-heading">
        <h2 id="side-summary-heading" className="side-heading">
          {props.summary.filtered ? 'Lastet opp i utvalget' : 'Lastet opp'}
        </h2>
        <p className="summary-big">{bytes(props.summary.uploaded)}</p>
        <p className="summary-sub">
          {props.summary.downloaded > 0
            ? `Ratio ${ratio(props.summary.uploaded / props.summary.downloaded)} · ${bytes(props.summary.downloaded)} ned`
            : 'Ingenting lastet ned'}
        </p>
        <button type="button" className="side-heading-action summary-link" onClick={props.onOpenStats}>
          Se statistikk
        </button>
      </section>

      <nav className="side-section" aria-labelledby="side-clients">
        <h2 id="side-clients" className="side-heading">
          Klienter
        </h2>
        <ul className="side-list">
          <li>
            <button
              type="button"
              className="side-item"
              aria-current={props.instanceFilter === 'all'}
              onClick={() => props.onInstanceFilter('all')}
            >
              <span className="side-item-main">
                <span className="side-item-name">Alle klienter</span>
              </span>
              <span className="side-count">{count(props.total)}</span>
            </button>
          </li>
          {instances.map((inst) => (
            <li key={inst.id} className="side-row">
              <button
                type="button"
                className="side-item"
                aria-current={props.instanceFilter === inst.id}
                onClick={() => props.onInstanceFilter(inst.id)}
              >
                <span className="inst-dot" style={{ background: colorOf(inst.id) }} aria-hidden="true" />
                <span className="side-item-main">
                  <span className="side-item-name">{inst.name}</span>
                  {inst.online ? (
                    <span className="side-item-sub">
                      {kindLabel(inst.kind)}
                      {inst.version ? ` ${inst.version}` : ''}
                    </span>
                  ) : (
                    <span className="side-item-sub side-item-problem">{instanceProblemShort(inst)}</span>
                  )}
                </span>
                <span className="side-count">{count(inst.count)}</span>
              </button>
              <button
                type="button"
                className="side-edit"
                onClick={() => props.onEdit(inst.id)}
                aria-label={`Endre ${inst.name}`}
              >
                Endre
              </button>
            </li>
          ))}
        </ul>
        <button type="button" className="btn side-add" onClick={props.onAdd}>
          Legg til klient
        </button>
      </nav>

      <nav className="side-section" aria-labelledby="side-states">
        <h2 id="side-states" className="side-heading">
          Status
        </h2>
        <ul className="side-list">
          {states.map((s) => (
            <li key={s}>
              <button
                type="button"
                className="side-item"
                aria-current={props.stateFilter === s}
                onClick={() => props.onStateFilter(s)}
              >
                {s !== 'all' && s !== 'active' && s !== 'duplicates' && (
                  <span className={`state-dot state-${s}`} aria-hidden="true" />
                )}
                <span className="side-item-main">
                  <span className="side-item-name">{stateFilterLabel(s)}</span>
                </span>
                <span className="side-count">{count(stateCounts[s])}</span>
              </button>
            </li>
          ))}
        </ul>
      </nav>

      {props.trackers.length > 0 && (
        <section className="side-section" aria-labelledby="side-trackers">
          <div className="side-heading-row">
            <h2 id="side-trackers" className="side-heading">
              Trackere
            </h2>
            {props.hiddenTrackers.size > 0 && (
              <button type="button" className="side-heading-action" onClick={props.onShowAllTrackers}>
                Vis alle
              </button>
            )}
          </div>
          <ul className="side-list">
            {props.trackers.map((t) => {
              const shown = !props.hiddenTrackers.has(t.name);
              const label = trackerLabel(t.name);
              return (
                <li key={t.name} className="side-row">
                  <label className={`side-item side-check${shown ? '' : ' is-hidden'}`}>
                    <input type="checkbox" checked={shown} onChange={() => props.onToggleTracker(t.name)} />
                    <span className="side-item-main">
                      <span className="side-item-name" title={label}>
                        {label}
                      </span>
                    </span>
                    <span className="side-count">{count(t.count)}</span>
                  </label>
                  {props.trackers.length > 1 && (
                    <button
                      type="button"
                      className="side-edit side-only"
                      onClick={() => props.onOnlyTracker(t.name)}
                      aria-label={`Vis bare ${label}`}
                    >
                      Bare denne
                    </button>
                  )}
                </li>
              );
            })}
          </ul>
        </section>
      )}
    </aside>
  );
}
