import { useEffect, useMemo, useRef, useState } from 'react';
import { api } from '../api';
import type { InstanceConfig, InstanceStatus, KindInfo, Torrent, TransferJob } from '../types';
import { count } from '../format';

interface Props {
  kinds: KindInfo[];
  instances: InstanceStatus[];
  colorOf: (instanceId: string) => string;
  torrents: Torrent[];
  onClose: () => void;
  onStarted: (job: TransferJob) => void;
  onEditInstance: (id: string) => void;
}

const plural = (n: number, one: string, many: string) => `${count(n)} ${n === 1 ? one : many}`;

export function MoveDialog({ kinds, instances, colorOf, torrents, onClose, onStarted, onEditInstance }: Props) {
  const ref = useRef<HTMLDivElement>(null);
  const [configs, setConfigs] = useState<InstanceConfig[] | null>(null);
  const [targetId, setTargetId] = useState('');
  const [removeFromSource, setRemoveFromSource] = useState(true);
  const [moveFiles, setMoveFiles] = useState(false);
  const [newPath, setNewPath] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    api
      .instances()
      .then(setConfigs)
      .catch((e: Error) => setError(e.message));
  }, []);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const target = e.target as Node | null;
      const aimedHere = target === document.body || (target !== null && ref.current?.contains(target));
      if (e.key === 'Escape' && e.isTrusted && !e.defaultPrevented && aimedHere) onClose();
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [onClose]);

  const nameOf = (id: string) => instances.find((i) => i.id === id)?.name ?? id;

  const plan = useMemo(() => {
    const kindOf = (kind: string) => kinds.find((k) => k.kind === kind);
    const onTarget = torrents.filter((t) => t.instanceId === targetId);
    const incomplete = torrents.filter((t) => t.instanceId !== targetId && t.progress < 1);
    const movable = torrents.filter((t) => t.instanceId !== targetId && t.progress >= 1);
    const sourceIds = [...new Set(movable.map((t) => t.instanceId))];
    const needsSSH = sourceIds.filter((id) => {
      const inst = instances.find((i) => i.id === id);
      const kind = inst && kindOf(inst.kind);
      const cfg = configs?.find((c) => c.id === id);
      const sshReady = Boolean(cfg?.sshHost && (cfg.torrentDir || !kind?.torrentDirHint));
      return kind?.ssh === 'required' && !sshReady;
    });
    const paths = [...new Set(movable.map((t) => t.savePath))];
    return { onTarget, incomplete, movable, sourceIds, needsSSH, paths };
  }, [torrents, targetId, instances, configs, kinds]);

  const sourcesLabel = [...new Set(torrents.map((t) => t.instanceId))].map(nameOf).join(', ');
  const ready =
    targetId !== '' &&
    configs !== null &&
    plan.movable.length > 0 &&
    plan.needsSSH.length === 0 &&
    (!moveFiles || newPath.trim().startsWith('/'));

  const start = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!ready) return;
    setBusy(true);
    setError(null);
    try {
      const job = await api.startTransfer({
        targetId,
        removeFromSource,
        newPath: moveFiles && removeFromSource ? newPath.trim() : '',
        items: plan.movable.map((t) => ({ instanceId: t.instanceId, hash: t.hash, name: t.name })),
      });
      onStarted(job);
    } catch (err) {
      setError((err as Error).message);
      setBusy(false);
    }
  };

  const verb = removeFromSource ? 'Flytt' : 'Kopier';
  const targetInst = instances.find((i) => i.id === targetId);
  const targetKind = targetInst && kinds.find((k) => k.kind === targetInst.kind);
  const targetCanMove = !targetKind || targetKind.canMoveFiles;

  return (
    <div className="modal-layer">
      <div ref={ref} className="dialog" role="dialog" aria-modal="true" aria-labelledby="move-dialog-title">
        <form className="dialog-body" onSubmit={start}>
          <div>
            <h2 className="dialog-title" id="move-dialog-title">
              Flytt til en annen klient
            </h2>
            <p className="hint">
              {plural(torrents.length, 'torrent', 'torrenter')} fra {sourcesLabel}
            </p>
          </div>

          <fieldset className="target-picker">
            <legend className="field-label">Til klient</legend>
            <div className="target-options">
              {instances.map((inst) => {
                const only = torrents.every((t) => t.instanceId === inst.id);
                return (
                  <label key={inst.id} className="target-option">
                    <input
                      type="radio"
                      name="target"
                      value={inst.id}
                      checked={targetId === inst.id}
                      disabled={only || !inst.online}
                      onChange={() => {
                        setTargetId(inst.id);
                        if (!kinds.find((k) => k.kind === inst.kind)?.canMoveFiles) setMoveFiles(false);
                      }}
                    />
                    <span className="inst-dot" style={{ background: colorOf(inst.id) }} aria-hidden="true" />
                    <span className="target-name">{inst.name}</span>
                    <span className="target-sub">
                      {!inst.online
                        ? 'Får ikke kontakt'
                        : only
                          ? 'Torrentene ligger her'
                          : kinds.find((k) => k.kind === inst.kind)?.label}
                    </span>
                  </label>
                );
              })}
            </div>
          </fieldset>

          {targetId && (plan.onTarget.length > 0 || plan.incomplete.length > 0) && (
            <p className="hint">
              Hoppes over:{' '}
              {[
                plan.onTarget.length > 0 && `${plural(plan.onTarget.length, 'ligger', 'ligger')} allerede der`,
                plan.incomplete.length > 0 &&
                  `${plural(plan.incomplete.length, 'er', 'er')} ikke ferdig nedlastet`,
              ]
                .filter(Boolean)
                .join(', ')}
              .
            </p>
          )}

          {plan.needsSSH.map((id) => (
            <div key={id} className="status-bad">
              <p>
                <strong>{nameOf(id)}</strong> kan ikke gi fra seg torrentfilene over webgrensesnittet. Stim må hente
                dem fra serveren over SSH.{' '}
                <button type="button" className="link" onClick={() => onEditInstance(id)}>
                  Sett opp SSH-tilgang
                </button>
              </p>
            </div>
          ))}

          <label className="check">
            <input
              type="checkbox"
              checked={removeFromSource}
              onChange={(e) => {
                setRemoveFromSource(e.target.checked);
                if (!e.target.checked) setMoveFiles(false);
              }}
            />
            <span>Fjern fra kildeklienten når målklienten har tatt over</span>
          </label>
          {!removeFromSource && (
            <p className="hint">Begge klientene seeder da de samme filene.</p>
          )}

          <label className="check">
            <input
              type="checkbox"
              checked={moveFiles}
              disabled={!removeFromSource || !targetCanMove}
              onChange={(e) => setMoveFiles(e.target.checked)}
            />
            <span>Flytt filene til en annen mappe</span>
          </label>
          {!targetCanMove && <p className="hint">{targetKind?.label} kan ikke flytte filer selv.</p>}
          {moveFiles && (
            <label className="field">
              <span className="field-label">Ny mappe, slik målklienten ser den</span>
              <input
                type="text"
                value={newPath}
                onChange={(e) => setNewPath(e.target.value)}
                placeholder={plan.paths[0] ?? '/home/bruker/ssd'}
                spellCheck={false}
                required
              />
              {plan.paths.length > 0 && (
                <span className="hint">Ligger nå i {plan.paths.slice(0, 3).join(', ')}</span>
              )}
            </label>
          )}

          <p className="hint">
            Målklienten sjekker filene før noe fjernes fra kilden. Finner den ikke alt, rulles flyttingen tilbake. Filer
            blir aldri slettet.
          </p>

          {error && <p className="status-bad">{error}</p>}

          <div className="dialog-actions">
            <span className="dialog-spacer" />
            <button type="button" className="btn" onClick={onClose}>
              Avbryt
            </button>
            <button type="submit" className="btn btn-primary" disabled={!ready || busy}>
              {plan.movable.length > 0 && targetId
                ? `${verb} ${plural(plan.movable.length, 'torrent', 'torrenter')}`
                : verb}
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
