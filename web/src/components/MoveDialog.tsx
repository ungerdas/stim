import { useEffect, useMemo, useRef, useState } from 'react';
import { api } from '../api';
import type { InstanceConfig, InstanceStatus, KindInfo, Torrent, TransferJob } from '../types';
import { bytes, count } from '../format';

interface Props {
  kinds: KindInfo[];
  instances: InstanceStatus[];
  colorOf: (instanceId: string) => string;
  torrents: Torrent[];
  onClose: () => void;
  onStarted: (job: TransferJob) => void;
  onEditInstance: (id: string) => void;
}

type Mode = 'same' | 'copy';

const plural = (n: number, one: string, many: string) => `${count(n)} ${n === 1 ? one : many}`;

export function MoveDialog({ kinds, instances, colorOf, torrents, onClose, onStarted, onEditInstance }: Props) {
  const ref = useRef<HTMLDivElement>(null);
  const [configs, setConfigs] = useState<InstanceConfig[] | null>(null);
  const [targetId, setTargetId] = useState('');
  const [mode, setMode] = useState<Mode>('same');
  const [removeFromSource, setRemoveFromSource] = useState(true);
  const [moveFiles, setMoveFiles] = useState(false);
  const [newPath, setNewPath] = useState('');
  const [targetPath, setTargetPath] = useState('');
  const [pathTouched, setPathTouched] = useState(false);
  const [deleteSourceFiles, setDeleteSourceFiles] = useState(false);
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

  // Suggest the target client's own download folder for copies, until the
  // user types something else.
  useEffect(() => {
    if (mode !== 'copy' || !targetId || pathTouched) return;
    let stale = false;
    api
      .savePath(targetId)
      .then((r) => {
        if (!stale && r.path) setTargetPath(r.path);
      })
      .catch(() => {});
    return () => {
      stale = true;
    };
  }, [mode, targetId, pathTouched]);

  const nameOf = (id: string) => instances.find((i) => i.id === id)?.name ?? id;
  const configOf = (id: string) => configs?.find((c) => c.id === id);
  const kindOf = (kind: string | undefined) => kinds.find((k) => k.kind === kind);

  const plan = useMemo(() => {
    const onTarget = torrents.filter((t) => t.instanceId === targetId);
    const incomplete = torrents.filter((t) => t.instanceId !== targetId && t.progress < 1);
    const movable = torrents.filter((t) => t.instanceId !== targetId && t.progress >= 1);
    const sourceIds = [...new Set(movable.map((t) => t.instanceId))];
    const cfg = (id: string) => configs?.find((c) => c.id === id);
    // Clients whose API cannot hand out .torrent files need SSH to be moved from at all.
    const needsSSH = sourceIds.filter((id) => {
      const inst = instances.find((i) => i.id === id);
      const kind = kinds.find((k) => k.kind === inst?.kind);
      const c = cfg(id);
      const sshReady = Boolean(c?.sshHost && (c.torrentDir || !kind?.torrentDirHint));
      return kind?.ssh === 'required' && !sshReady;
    });
    const targetServer = cfg(targetId)?.server ?? '';
    const otherServer = sourceIds.filter((id) => {
      const s = cfg(id)?.server ?? '';
      return s !== '' && targetServer !== '' && s !== targetServer;
    });
    const paths = [...new Set(movable.map((t) => t.savePath))];
    const size = movable.reduce((sum, t) => sum + t.size, 0);
    return { onTarget, incomplete, movable, sourceIds, needsSSH, otherServer, paths, size };
  }, [torrents, targetId, instances, configs, kinds]);

  const copying = mode === 'copy';
  const targetConfig = configOf(targetId);
  const targetNotReady = copying && targetConfig !== undefined && !targetConfig.sshReady;
  const sourcesWithoutSSH = copying ? plan.sourceIds.filter((id) => !configOf(id)?.sshHost) : [];

  const pickTarget = (id: string) => {
    setTargetId(id);
    if (!kindOf(instances.find((i) => i.id === id)?.kind)?.canMoveFiles) setMoveFiles(false);
    // Default to copying when the torrents live on another machine.
    const server = configOf(id)?.server ?? '';
    const elsewhere = torrents.some((t) => {
      const s = configOf(t.instanceId)?.server ?? '';
      return t.instanceId !== id && s !== '' && server !== '' && s !== server;
    });
    setMode(elsewhere ? 'copy' : 'same');
    if (!pathTouched) setTargetPath('');
  };

  const sourcesLabel = [...new Set(torrents.map((t) => t.instanceId))].map(nameOf).join(', ');
  const ready =
    targetId !== '' &&
    configs !== null &&
    plan.movable.length > 0 &&
    plan.needsSSH.length === 0 &&
    (copying
      ? targetPath.trim().startsWith('/') && !targetNotReady && sourcesWithoutSSH.length === 0
      : !moveFiles || newPath.trim().startsWith('/'));

  const start = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!ready) return;
    setBusy(true);
    setError(null);
    try {
      const job = await api.startTransfer({
        targetId,
        removeFromSource,
        newPath: !copying && moveFiles && removeFromSource ? newPath.trim() : '',
        copyFiles: copying,
        targetPath: copying ? targetPath.trim() : '',
        deleteSourceFiles: copying && removeFromSource && deleteSourceFiles,
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
  const targetKind = kindOf(targetInst?.kind);
  const targetCanMove = !targetKind || targetKind.canMoveFiles;

  const setUp = (id: string, why: string) => (
    <div key={id} className="status-bad">
      <p>
        <strong>{nameOf(id)}</strong> {why}{' '}
        <button type="button" className="link" onClick={() => onEditInstance(id)}>
          Sett opp SSH-tilgang
        </button>
      </p>
    </div>
  );

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
                const server = configOf(inst.id)?.server;
                return (
                  <label key={inst.id} className="target-option">
                    <input
                      type="radio"
                      name="target"
                      value={inst.id}
                      checked={targetId === inst.id}
                      disabled={only || !inst.online}
                      onChange={() => pickTarget(inst.id)}
                    />
                    <span className="inst-dot" style={{ background: colorOf(inst.id) }} aria-hidden="true" />
                    <span className="target-name">{inst.name}</span>
                    <span className="target-sub">
                      {!inst.online
                        ? 'Får ikke kontakt'
                        : only
                          ? 'Torrentene ligger her'
                          : server
                            ? `${kindOf(inst.kind)?.label} på ${server}`
                            : kindOf(inst.kind)?.label}
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

          {plan.needsSSH.map((id) =>
            setUp(
              id,
              'kan ikke gi fra seg torrentfilene over webgrensesnittet. Stim må hente dem fra serveren over SSH.',
            ),
          )}

          {targetId && (
            <fieldset className="target-picker">
              <legend className="field-label">Filene</legend>
              <div className="target-options">
                <label className="target-option">
                  <input type="radio" name="mode" checked={!copying} onChange={() => setMode('same')} />
                  <span className="target-name">Samme server</span>
                  <span className="target-sub">Målklienten bruker filene der de ligger</span>
                </label>
                <label className="target-option">
                  <input type="radio" name="mode" checked={copying} onChange={() => setMode('copy')} />
                  <span className="target-name">En annen server</span>
                  <span className="target-sub">Stim kopierer filene over SSH</span>
                </label>
              </div>
            </fieldset>
          )}

          {targetId && !copying && plan.otherServer.length > 0 && (
            <p className="status-bad">
              {plan.otherServer.map(nameOf).join(', ')} ser ut til å kjøre på en annen server enn {nameOf(targetId)}.
              Da finner ikke målklienten filene, og flyttingen rulles tilbake. Velg «En annen server» for å kopiere dem.
            </p>
          )}

          {copying && targetNotReady &&
            setUp(targetId, 'trenger SSH-tilgang som er testet og lagret, så Stim kan skrive filene til serveren.')}
          {sourcesWithoutSSH.map((id) => setUp(id, 'trenger SSH-tilgang, så Stim kan lese filene på serveren.'))}

          <label className="check">
            <input
              type="checkbox"
              checked={removeFromSource}
              onChange={(e) => {
                setRemoveFromSource(e.target.checked);
                if (!e.target.checked) {
                  setMoveFiles(false);
                  setDeleteSourceFiles(false);
                }
              }}
            />
            <span>Fjern fra kildeklienten når målklienten har tatt over</span>
          </label>
          {!removeFromSource && (
            <p className="hint">
              {copying ? 'Begge klientene seeder da hver sin kopi.' : 'Begge klientene seeder da de samme filene.'}
            </p>
          )}

          {copying ? (
            <>
              <label className="field">
                <span className="field-label">Mappe på den nye serveren, slik {nameOf(targetId)} ser den</span>
                <input
                  type="text"
                  value={targetPath}
                  onChange={(e) => {
                    setTargetPath(e.target.value);
                    setPathTouched(true);
                  }}
                  placeholder="/home/bruker/downloads"
                  spellCheck={false}
                  required
                />
                <span className="hint">
                  Torrentene legges rett i denne mappen. {bytes(plan.size)} skal kopieres.
                </span>
              </label>
              <label className="check">
                <input
                  type="checkbox"
                  checked={deleteSourceFiles}
                  disabled={!removeFromSource}
                  onChange={(e) => setDeleteSourceFiles(e.target.checked)}
                />
                <span>Slett filene på den gamle serveren etterpå</span>
              </label>
              {deleteSourceFiles && (
                <p className="hint">
                  Først når målklienten har sjekket kopien. Filer som andre torrenter på den gamle serveren også bruker,
                  blir liggende.
                </p>
              )}
            </>
          ) : (
            <>
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
                  <span className="hint">
                    {plan.paths.length > 0 && `Ligger nå i ${plan.paths.slice(0, 3).join(', ')}. `}
                    Filer som andre torrenter også bruker, for eksempel samme utgivelse på en annen tracker, blir
                    liggende der de er.
                  </span>
                </label>
              )}
            </>
          )}

          <p className="hint">
            {copying
              ? 'Filene går fra den gamle serveren via Stim til den nye. Målklienten sjekker kopien før noe fjernes fra kilden. Feiler noe, slettes kopien, og kilden er urørt.'
              : 'Målklienten sjekker filene før noe fjernes fra kilden. Finner den ikke alt, rulles flyttingen tilbake. Filer blir aldri slettet.'}
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
