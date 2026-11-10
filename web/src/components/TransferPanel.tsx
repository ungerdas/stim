import { useState } from 'react';
import type { InstanceStatus, TransferJob } from '../types';
import { count, percent } from '../format';
import { transferProblem, transferStateLabel } from '../text';

interface Props {
  jobs: TransferJob[];
  instances: InstanceStatus[];
  colorOf: (instanceId: string) => string;
  onCancel: (id: string) => void;
  onDismiss: (id: string) => void;
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
  // Each finished item counts fully; the one in progress counts by its check.
  const progress = (done + failed + cancelled + (current?.state === 'checking' ? current.progress : 0)) / total;

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
          {running && current && !job.cancelling && (
            <span className="transfer-current">
              {' '}
              ({transferStateLabel[current.state].toLowerCase()}
              {current.state === 'checking' ? ` ${percent(current.progress)}` : ''})
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
          {job.items.map((item) => {
            const problem = item.state === 'failed' ? transferProblem(item) : null;
            return (
              <li key={`${item.sourceId}:${item.hash}`} className={`transfer-item state-${item.state}`}>
                <span className="transfer-item-name" title={item.name}>
                  {item.name || item.hash}
                </span>
                <span className="transfer-item-from">fra {nameOf(item.sourceId)}</span>
                <span className="transfer-item-state">
                  {transferStateLabel[item.state]}
                  {item.state === 'checking' ? ` ${percent(item.progress)}` : ''}
                </span>
                {problem && (
                  <span className="transfer-item-problem" title={problem.detail}>
                    {problem.text}
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
