import { useState } from 'react';
import type { InstanceStatus, TransferItem, TransferJob } from '../types';
import { count, eta, percent, rate } from '../format';
import { transferNote, transferProblem, transferStateLabel } from '../text';

interface Props {
  jobs: TransferJob[];
  instances: InstanceStatus[];
  colorOf: (instanceId: string) => string;
  onCancel: (id: string) => void;
  onDismiss: (id: string) => void;
}

const copyShare = (item: TransferItem) => (item.copyTotal ? (item.copyDone ?? 0) / item.copyTotal : 0);

/** How far one item has come, 0..1. With copying, the copy is most of the work. */
function itemProgress(item: TransferItem, copyFiles: boolean): number {
  if (['done', 'failed', 'cancelled'].includes(item.state)) return 1;
  if (!copyFiles) return item.state === 'checking' ? item.progress : 0;
  switch (item.state) {
    case 'copying':
      return 0.8 * copyShare(item);
    case 'queued':
    case 'fetching':
      return 0;
    case 'checking':
      return 0.8 + 0.2 * item.progress;
    default:
      return 0.8;
  }
}

/** "kopierer filer 41,2 %, 38 MiB/s, 4 min igjen", or the state alone. */
function stateDetail(item: TransferItem): string {
  if (item.state === 'checking') return ` ${percent(item.progress)}`;
  if (item.state !== 'copying' || !item.copyTotal) return '';
  const parts = [percent(copyShare(item))];
  const speed = rate(item.copyRate ?? 0);
  if (speed) {
    parts.push(speed);
    const left = (item.copyTotal - (item.copyDone ?? 0)) / (item.copyRate ?? 1);
    if (left >= 1) parts.push(`${eta(left)} igjen`);
  }
  return ` ${parts.join(', ')}`;
}

/** Running and recently finished moves, newest first. */
export function TransferPanel({ jobs, instances, colorOf, onCancel, onDismiss }: Props) {
  if (jobs.length === 0) return null;
  return (
    <section className="transfers" aria-label="Flytting mellom klienter">
      {jobs.map((job) => (
        <JobRow
          key={job.id}
          job={job}
          instances={instances}
          colorOf={colorOf}
          onCancel={onCancel}
          onDismiss={onDismiss}
        />
      ))}
    </section>
  );
}

function JobRow({ job, instances, colorOf, onCancel, onDismiss }: { job: TransferJob } & Omit<Props, 'jobs'>) {
  const [open, setOpen] = useState(false);
  const nameOf = (id: string) => instances.find((i) => i.id === id)?.name ?? 'en fjernet klient';

  const total = job.items.length;
  const done = job.items.filter((i) => i.state === 'done').length;
  const failed = job.items.filter((i) => i.state === 'failed').length;
  const cancelled = job.items.filter((i) => i.state === 'cancelled').length;
  const running = job.finishedAt === 0;
  const current = job.items.find((i) => !['queued', 'done', 'failed', 'cancelled'].includes(i.state));
  const copyFiles = Boolean(job.copyFiles);
  const progress = job.items.reduce((sum, item) => sum + itemProgress(item, copyFiles), 0) / total;
  const notes = job.items.filter((i) => i.state === 'done' && i.note).length;

  const verb = job.removeFromSource ? 'Flytter' : 'Kopierer';
  const verbDone = job.removeFromSource ? 'Flyttet' : 'Kopierte';
  let summary: string;
  if (running) {
    summary = job.cancelling
      ? 'Avbryter etter torrenten som er i gang …'
      : `${verb} ${count(done + failed + 1 > total ? total : done + failed + 1)} av ${count(total)}`;
  } else {
    summary = `${verbDone} ${count(done)} av ${count(total)}`;
  }
  const extras = [failed > 0 && `${count(failed)} feilet`, cancelled > 0 && `${count(cancelled)} avbrutt`].filter(
    Boolean,
  );

  return (
    <div className={`transfer${failed > 0 ? ' has-failures' : ''}`}>
      <div className="transfer-head">
        <span className="inst-dot" style={{ background: colorOf(job.targetId) }} aria-hidden="true" />
        <p className="transfer-summary">
          <strong>Til {nameOf(job.targetId)}:</strong> {summary}
          {extras.length > 0 && <span className="transfer-bad">, {extras.join(', ')}</span>}
          {!running && notes > 0 && (
            <span className="transfer-current">, {notes === 1 ? 'én merknad' : `${count(notes)} merknader`}</span>
          )}
          {running && current && !job.cancelling && (
            <span className="transfer-current">
              {' '}
              ({transferStateLabel[current.state].toLowerCase()}
              {stateDetail(current)})
            </span>
          )}
        </p>
        <span className="bar transfer-bar" aria-hidden="true">
          <span className="bar-fill" style={{ width: `${Math.round(progress * 100)}%` }} />
        </span>
        <button type="button" className="btn btn-small" onClick={() => setOpen((o) => !o)} aria-expanded={open}>
          {open ? 'Skjul' : 'Detaljer'}
        </button>
        {running ? (
          <button
            type="button"
            className="btn btn-small"
            onClick={() => onCancel(job.id)}
            disabled={job.cancelling}
          >
            Avbryt
          </button>
        ) : (
          <button type="button" className="btn btn-small" onClick={() => onDismiss(job.id)}>
            Lukk
          </button>
        )}
      </div>
      {open && (
        <ul className="transfer-items">
          {job.copyFiles && job.targetPath && (
            <li className="transfer-item transfer-where">
              Kopieres til {job.targetPath} på serveren til {nameOf(job.targetId)}
              {job.deleteSourceFiles ? ', og slettes fra den gamle serveren etterpå' : ''}
            </li>
          )}
          {job.items.map((item) => {
            const problem = item.state === 'failed' ? transferProblem(item, copyFiles) : null;
            const note = item.state === 'done' ? transferNote(item) : null;
            return (
              <li key={`${item.sourceId}:${item.hash}`} className={`transfer-item state-${item.state}`}>
                <span className="transfer-item-name" title={item.name}>
                  {item.name || item.hash}
                </span>
                <span className="transfer-item-from">fra {nameOf(item.sourceId)}</span>
                <span className="transfer-item-state">
                  {transferStateLabel[item.state]}
                  {stateDetail(item)}
                </span>
                {problem && (
                  <span className="transfer-item-problem" title={problem.detail}>
                    {problem.text}
                  </span>
                )}
                {note && (
                  <span className="transfer-item-note" title={item.noteDetail}>
                    {note}
                  </span>
                )}
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}
