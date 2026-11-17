import { useEffect, useState } from 'react';
import { api } from '../api';
import type { InstanceConfig, InstanceStatus, KindInfo } from '../types';
import { bytes, count } from '../format';
import { instanceProblemShort } from '../text';

interface Props {
  instances: InstanceStatus[];
  kinds: KindInfo[];
  colorOf: (instanceId: string) => string;
  grouping: boolean;
  onGrouping: (on: boolean) => void;
  onAdd: () => void;
  onEdit: (id: string) => void;
  onCleanUp: (server: string) => void;
  onBack: () => void;
  /** Bumped when a client is saved, so the SSH details are read again. */
  revision: number;
}

const buildStamp = new Intl.DateTimeFormat('nb-NO', {
  day: 'numeric',
  month: 'long',
  year: 'numeric',
  hour: '2-digit',
  minute: '2-digit',
}).format(new Date(__BUILD_TIME__));

/** Setup that is not needed day to day: clients, servers, display, version. */
export function SettingsPage(props: Props) {
  const { instances, kinds, colorOf } = props;
  const [configs, setConfigs] = useState<InstanceConfig[] | null>(null);

  useEffect(() => {
    api
      .instances()
      .then(setConfigs)
      .catch(() => setConfigs([]));
  }, [props.revision, instances.length]);

  const kindOf = (kind: string) => kinds.find((k) => k.kind === kind);
  const configOf = (id: string) => configs?.find((c) => c.id === id);

  const sshText = (inst: InstanceStatus): { text: string; warn: boolean } => {
    const cfg = configOf(inst.id);
    const needed = kindOf(inst.kind)?.ssh === 'required';
    if (!cfg) return { text: '', warn: false };
    if (cfg.sshReady) return { text: 'SSH klar', warn: false };
    if (cfg.sshHost) return { text: 'SSH ikke testet', warn: true };
    return { text: needed ? 'Uten SSH, trengs for å flytte herfra' : 'Uten SSH', warn: needed };
  };

  const servers = [...new Set(instances.map((i) => i.server).filter(Boolean))].sort().map((name) => {
    const here = instances.filter((i) => i.server === name);
    // As shown: two clients on the same disk report a few bytes apart.
    const free = [...new Set(here.map((i) => i.freeSpace).filter((n) => n >= 0).sort((a, b) => a - b).map(bytes))];
    const sshReady = here.some((i) => configOf(i.id)?.sshReady);
    return { name, here, free, sshReady };
  });

  return (
    <div className="settings">
      <header className="settings-head">
        <button type="button" className="link settings-back" onClick={props.onBack}>
          Tilbake til torrentene
        </button>
        <h1 className="settings-title">Innstillinger</h1>
      </header>

      <section className="settings-section" aria-labelledby="settings-clients">
        <div className="settings-section-head">
          <h2 id="settings-clients" className="settings-heading">
            Klienter
          </h2>
          <button type="button" className="btn" onClick={props.onAdd}>
            Legg til klient
          </button>
        </div>
        {instances.length === 0 ? (
          <p className="hint">Ingen klienter ennå.</p>
        ) : (
          <ul className="settings-list">
            {instances.map((inst) => {
              const ssh = sshText(inst);
              const kind = kindOf(inst.kind)?.label ?? inst.kind;
              return (
                <li key={inst.id} className="settings-client">
                  <span className="inst-dot" style={{ background: colorOf(inst.id) }} aria-hidden="true" />
                  <span className="settings-client-main">
                    <span className="settings-client-name">{inst.name}</span>
                    <span className="settings-client-kind">
                      {kind}
                      {inst.version ? ` ${inst.version}` : ''}
                    </span>
                  </span>
                  {inst.online ? (
                    <span className="settings-client-facts">
                      <span>{count(inst.count)} torrenter</span>
                      {inst.freeSpace >= 0 && <span>{bytes(inst.freeSpace)} ledig</span>}
                    </span>
                  ) : (
                    <span className="settings-client-facts settings-bad">{instanceProblemShort(inst)}</span>
                  )}
                  <span className={`settings-client-ssh${ssh.warn ? ' settings-warn' : ''}`}>{ssh.text}</span>
                  <button type="button" className="btn btn-small settings-edit" onClick={() => props.onEdit(inst.id)}>
                    Endre
                  </button>
                </li>
              );
            })}
          </ul>
        )}
      </section>

      {servers.length > 0 && (
        <section className="settings-section" aria-labelledby="settings-servers">
          <h2 id="settings-servers" className="settings-heading">
            Servere
          </h2>
          <p className="hint">
            Klienter med samme SSH-vert, eller samme vert i adressen når SSH ikke er satt opp, regnes som samme
            server. Der deler de disk, og der ser Stim etter filer som ingen torrent bruker.
          </p>
          <ul className="settings-list">
            {servers.map((s) => (
              <li key={s.name} className="settings-server">
                <span className="settings-server-main">
                  <span className="settings-client-name">{s.name}</span>
                  <span className="settings-client-kind">{s.here.map((i) => i.name).join(', ')}</span>
                </span>
                <span className="settings-client-facts">
                  {s.free.length > 0 && (
                    <span>
                      {s.free.length === 1
                        ? `${s.free[0]} ledig`
                        : `${s.free[0]} til ${s.free[s.free.length - 1]} ledig`}
                    </span>
                  )}
                </span>
                <button
                  type="button"
                  className="btn btn-small"
                  onClick={() => props.onCleanUp(s.name)}
                  disabled={configs !== null && !s.sshReady}
                  title={configs !== null && !s.sshReady ? 'Krever SSH på en av klientene på serveren' : undefined}
                >
                  Finn filer uten torrent
                </button>
              </li>
            ))}
          </ul>
        </section>
      )}

      <section className="settings-section" aria-labelledby="settings-display">
        <h2 id="settings-display" className="settings-heading">
          Visning
        </h2>
        <label className="settings-toggle">
          <input type="checkbox" checked={props.grouping} onChange={(e) => props.onGrouping(e.target.checked)} />
          <span>
            <span className="settings-toggle-name">Samle kryss-seeds</span>
            <span className="hint">
              Samme utgivelse på flere trackere vises som én rad, med trackerne, summert opplastet og samlet ratio. Pilen
              foran navnet folder den ut.
            </span>
          </span>
        </label>
        <p className="hint">Valgene huskes i denne nettleseren.</p>
      </section>

      <section className="settings-section" aria-labelledby="settings-about">
        <h2 id="settings-about" className="settings-heading">
          Om Stim
        </h2>
        <p className="settings-about">
          Bygget {buildStamp}.{' '}
          <a href="https://github.com/ungerdas/stim" target="_blank" rel="noreferrer">
            Kildekode og dokumentasjon på GitHub
          </a>
        </p>
      </section>
    </div>
  );
}
