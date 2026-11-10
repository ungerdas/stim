import type { InstanceStatus, KindInfo, State } from '../types';
import { count } from '../format';
import { instanceProblemShort, stateLabel } from '../text';

export type StateFilter = State | 'all' | 'active';

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
}

// Always shown, then any other state that currently has torrents.
const FIXED_STATES: StateFilter[] = ['all', 'active', 'downloading', 'seeding', 'paused', 'error'];
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
  return stateLabel[s];
}

export function Sidebar(props: Props) {
  const { instances, kinds, colorOf, stateCounts } = props;
  const kindLabel = (kind: string) => kinds.find((k) => k.kind === kind)?.label ?? kind;
  const states = [...FIXED_STATES, ...EXTRA_STATES.filter((s) => stateCounts[s] > 0)];

  return (
    <aside className="sidebar">
      <div className="brand">
        <span className="brand-name">Stim</span>
        <span className="side-build">Bygget {buildStamp}</span>
      </div>

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
                {s !== 'all' && s !== 'active' && <span className={`state-dot state-${s}`} aria-hidden="true" />}
                <span className="side-item-main">
                  <span className="side-item-name">{stateFilterLabel(s)}</span>
                </span>
                <span className="side-count">{count(stateCounts[s])}</span>
              </button>
            </li>
          ))}
        </ul>
      </nav>
    </aside>
  );
}
