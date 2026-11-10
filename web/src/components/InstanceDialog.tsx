import { useEffect, useRef, useState } from 'react';
import { api } from '../api';
import type { InstanceConfig, InstanceInput, KindInfo, SSHTestResult, TestResult } from '../types';
import { connectionProblem } from '../text';

interface Props {
  kinds: KindInfo[];
  /** The instance being edited, or null when adding a new one. */
  existing: InstanceConfig | null;
  onClose: () => void;
  /** Called after a client was added, saved or removed. */
  onChanged: (message: string) => void;
}

interface FormState {
  kind: string;
  name: string;
  url: string;
  username: string;
  password: string;
  basicUser: string;
  basicPass: string;
  tlsSkipVerify: boolean;
  sshHost: string;
  sshPort: string;
  sshUser: string;
  sshPassword: string;
  sshKey: string;
  torrentDir: string;
}

/** The host part of a client URL, to suggest as the SSH host. */
function hostOf(url: string): string {
  try {
    return new URL(url).hostname;
  } catch {
    return '';
  }
}

export function InstanceDialog({ kinds, existing, onClose, onChanged }: Props) {
  const ref = useRef<HTMLDivElement>(null);
  const [form, setForm] = useState<FormState>(() => ({
    kind: existing?.kind ?? kinds[0]?.kind ?? '',
    name: existing?.name ?? '',
    url: existing?.url ?? '',
    username: existing?.username ?? '',
    password: '',
    basicUser: existing?.basicUser ?? '',
    basicPass: '',
    tlsSkipVerify: existing?.tlsSkipVerify ?? false,
    sshHost: existing?.sshHost ?? '',
    sshPort: existing?.sshPort ? String(existing.sshPort) : '',
    sshUser: existing?.sshUser ?? '',
    sshPassword: '',
    sshKey: '',
    torrentDir: existing?.torrentDir ?? '',
  }));
  // The SSH host key to pin when saving: the one seen in a test on first
  // contact, or a changed one the user has accepted.
  const [pinKey, setPinKey] = useState<{ key: string; fingerprint: string; replace: boolean } | null>(null);
  const [busy, setBusy] = useState<'test' | 'save' | 'delete' | null>(null);
  const [test, setTest] = useState<TestResult | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [confirmDelete, setConfirmDelete] = useState(false);

  // Escape closes the form. When editing, nothing is auto-focused by the
  // markup, so move focus into the form for keyboard users.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      // Only a real key press aimed at the form (or at nothing in particular)
      // closes it: Escape pressed inside an extension's own popup, or a
      // synthetic key event dispatched by one, must not.
      const target = e.target as Node | null;
      const aimedHere = target === document.body || (target !== null && ref.current?.contains(target));
      if (e.key === 'Escape' && e.isTrusted && !e.defaultPrevented && aimedHere) onClose();
    };
    document.addEventListener('keydown', onKey);
    if (!ref.current?.contains(document.activeElement)) {
      ref.current?.querySelector<HTMLInputElement>('input[type="text"]')?.focus();
    }
    return () => document.removeEventListener('keydown', onKey);
  }, [onClose]);

  const kind = kinds.find((k) => k.kind === form.kind);
  // SSH access only matters for clients whose API cannot export torrents.
  const showSSH = Boolean(kind?.ssh);
  // Clients that say where each .torrent file is need no folder.
  const needsDir = Boolean(kind?.torrentDirHint);
  const set = <K extends keyof FormState>(key: K, value: FormState[K]) => {
    setForm((f) => ({ ...f, [key]: value }));
    setTest(null);
    setError(null);
    if (key === 'sshHost' || key === 'sshPort') setPinKey(null);
  };

  // Blank password fields are left out, which keeps the stored password.
  const input = (): InstanceInput => ({
    id: existing?.id,
    kind: form.kind,
    name: form.name.trim(),
    url: form.url.trim(),
    username: kind?.needsUsername ? form.username.trim() : '',
    password: form.password !== '' || !existing ? form.password : undefined,
    basicUser: form.basicUser.trim(),
    basicPass: form.basicPass !== '' || !existing ? form.basicPass : undefined,
    tlsSkipVerify: form.tlsSkipVerify,
    sshHost: showSSH ? form.sshHost.trim() : '',
    sshPort: Number.parseInt(form.sshPort, 10) || 0,
    sshUser: form.sshUser.trim(),
    sshPassword: form.sshPassword !== '' || !existing ? form.sshPassword : undefined,
    sshKey: form.sshKey.trim() !== '' || !existing ? form.sshKey : undefined,
    torrentDir: needsDir ? form.torrentDir.trim() : '',
    sshHostKey: pinKey?.key,
    forgetHostKey: pinKey?.replace,
  });

  const runTest = async () => {
    setBusy('test');
    setTest(null);
    setError(null);
    try {
      const result = await api.testInstance(input());
      setTest(result);
      const ssh = result.ssh;
      if (ssh?.newHostKey && ssh.hostKey && ssh.fingerprint && !pinKey?.replace) {
        setPinKey({ key: ssh.hostKey, fingerprint: ssh.fingerprint, replace: false });
      }
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(null);
    }
  };

  const save = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy('save');
    setError(null);
    try {
      if (existing) {
        await api.updateInstance(existing.id, input());
        onChanged(`Lagret ${form.name.trim()}`);
      } else {
        await api.createInstance(input());
        onChanged(`La til ${form.name.trim()}`);
      }
    } catch (e) {
      setError((e as Error).message);
      setBusy(null);
    }
  };

  const remove = async () => {
    if (!existing) return;
    setBusy('delete');
    setError(null);
    try {
      await api.deleteInstance(existing.id);
      onChanged(`Fjernet ${existing.name}`);
    } catch (e) {
      setError((e as Error).message);
      setBusy(null);
    }
  };

  const advancedOpen = Boolean(existing?.basicUser || existing?.tlsSkipVerify);
  const storedFingerprint = existing?.sshHost === form.sshHost.trim() ? existing?.sshHostFingerprint : '';

  const sshStatus = (ssh: SSHTestResult) => {
    if (ssh.ok) {
      return (
        <p className="status-ok">
          {needsDir
            ? `SSH: fant ${ssh.torrents} torrentfiler i ${form.torrentDir.trim()}.`
            : 'SSH-tilkoblingen virker.'}
          {pinKey && !pinKey.replace && (
            <>
              {' '}
              Serverens nøkkel er {pinKey.fingerprint}. Den lagres sammen med klienten, og Stim nekter senere å koble til
              hvis den endres.
            </>
          )}
        </p>
      );
    }
    if (ssh.hostKeyChanged) {
      return (
        <div className="status-bad">
          <p>
            Serverens SSH-nøkkel er endret siden den ble lagret. Det skjer når serveren er installert på nytt, men kan
            også bety at noen står mellom deg og serveren. Godta bare hvis du vet hvorfor.
          </p>
          <p>Ny nøkkel: {ssh.fingerprint}</p>
          {ssh.hostKey && ssh.fingerprint && (
            <button
              type="button"
              className="btn btn-small"
              onClick={() => {
                setPinKey({ key: ssh.hostKey!, fingerprint: ssh.fingerprint!, replace: true });
                setTest(null);
              }}
            >
              Godta ny nøkkel
            </button>
          )}
        </div>
      );
    }
    return <p className="status-bad">SSH: {ssh.error}</p>;
  };

  return (
    // A plain layer, deliberately not a native modal <dialog>. showModal()
    // makes everything outside the dialog inert and puts it in the browser's
    // top layer, so anything a browser extension adds to the page while it is
    // open (a password manager's paste prompt, say) ends up underneath it and
    // cannot be clicked. Nothing closes the form except the × button, Escape,
    // or a successful save.
    <div className="modal-layer">
      <div
        ref={ref}
        className="dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby="instance-dialog-title"
      >
        <form className="dialog-body" onSubmit={save}>
          <h2 className="dialog-title" id="instance-dialog-title">{existing ? `Endre ${existing.name}` : 'Legg til klient'}</h2>

          <fieldset className="kind-picker">
            <legend className="field-label">Klienttype</legend>
            <div className="kind-options">
              {kinds.map((k) => (
                <label key={k.kind} className="kind-option">
                  <input
                    type="radio"
                    name="kind"
                    value={k.kind}
                    checked={form.kind === k.kind}
                    onChange={() => set('kind', k.kind)}
                  />
                  <span>{k.label}</span>
                </label>
              ))}
            </div>
          </fieldset>

          <label className="field">
            <span className="field-label">Navn</span>
            <input
              type="text"
              value={form.name}
              onChange={(e) => set('name', e.target.value)}
              placeholder="F.eks. Seedboks Deluge"
              required
              autoFocus={!existing}
            />
          </label>

          <label className="field">
            <span className="field-label">Adresse til webgrensesnittet</span>
            <input
              type="url"
              value={form.url}
              onChange={(e) => set('url', e.target.value)}
              placeholder={kind?.urlHint}
              required
              spellCheck={false}
            />
          </label>

          <div className="field-row">
            {kind?.needsUsername && (
              <label className="field">
                <span className="field-label">Brukernavn</span>
                <input
                  type="text"
                  value={form.username}
                  onChange={(e) => set('username', e.target.value)}
                  autoComplete="off"
                  spellCheck={false}
                />
              </label>
            )}
            <label className="field">
              <span className="field-label">Passord</span>
              <input
                type="password"
                value={form.password}
                onChange={(e) => set('password', e.target.value)}
                placeholder={existing?.hasPassword ? 'Uendret' : ''}
                autoComplete="new-password"
              />
            </label>
          </div>

          <details className="advanced" open={advancedOpen}>
            <summary>Bak revers proxy eller med selvsignert sertifikat</summary>
            <p className="hint">
              Fyll ut dette hvis nettleseren spør om brukernavn og passord før du i det hele tatt ser klienten, slik mange
              seedbokser gjør.
            </p>
            <div className="field-row">
              <label className="field">
                <span className="field-label">HTTP-brukernavn</span>
                <input
                  type="text"
                  value={form.basicUser}
                  onChange={(e) => set('basicUser', e.target.value)}
                  autoComplete="off"
                  spellCheck={false}
                />
              </label>
              <label className="field">
                <span className="field-label">HTTP-passord</span>
                <input
                  type="password"
                  value={form.basicPass}
                  onChange={(e) => set('basicPass', e.target.value)}
                  placeholder={existing?.hasBasicPass ? 'Uendret' : ''}
                  autoComplete="new-password"
                />
              </label>
            </div>
            <label className="check">
              <input
                type="checkbox"
                checked={form.tlsSkipVerify}
                onChange={(e) => set('tlsSkipVerify', e.target.checked)}
              />
              <span>Godta selvsignert sertifikat</span>
            </label>
          </details>

          {showSSH && (
            <details className="advanced" open={Boolean(existing?.sshHost)}>
              <summary>
                {kind?.ssh === 'optional'
                  ? 'SSH-tilgang, valgfritt'
                  : 'SSH-tilgang, for å flytte torrenter fra denne klienten'}
              </summary>
              {kind?.ssh === 'optional' ? (
                <p className="hint">
                  Bak ruTorrent får ikke Stim hente torrentfiler eller slette filer gjennom {kind?.label}. Med SSH-tilgang
                  gjør Stim det over SFTP i stedet: leser torrentfilen når du flytter en torrent herfra, og sletter
                  filene når du velger «Slett torrenter og filer». Over en ren /RPC2-adresse trengs det ikke.
                </p>
              ) : (
                <p className="hint">
                  {kind?.label} kan ikke gi fra seg torrentfiler over webgrensesnittet. For å flytte torrenter herfra til
                  en annen klient henter Stim filene fra serveren over SFTP. Stim leser bare, og bare .torrent-filer.
                </p>
              )}
              <div className="field-row">
                <label className="field field-grow">
                  <span className="field-label">SSH-vert</span>
                  <input
                    type="text"
                    value={form.sshHost}
                    onChange={(e) => set('sshHost', e.target.value)}
                    placeholder={hostOf(form.url) || 'server.example.com'}
                    autoComplete="off"
                    spellCheck={false}
                  />
                </label>
                <label className="field field-port">
                  <span className="field-label">Port</span>
                  <input
                    type="text"
                    inputMode="numeric"
                    value={form.sshPort}
                    onChange={(e) => set('sshPort', e.target.value.replace(/\D/g, ''))}
                    placeholder="22"
                  />
                </label>
              </div>
              <div className="field-row">
                <label className="field">
                  <span className="field-label">SSH-brukernavn</span>
                  <input
                    type="text"
                    value={form.sshUser}
                    onChange={(e) => set('sshUser', e.target.value)}
                    autoComplete="off"
                    spellCheck={false}
                  />
                </label>
                <label className="field">
                  <span className="field-label">SSH-passord</span>
                  <input
                    type="password"
                    value={form.sshPassword}
                    onChange={(e) => set('sshPassword', e.target.value)}
                    placeholder={existing?.hasSshPassword ? 'Uendret' : ''}
                    autoComplete="new-password"
                  />
                </label>
              </div>
              <label className="field">
                <span className="field-label">Privat nøkkel, i stedet for passord</span>
                <textarea
                  value={form.sshKey}
                  onChange={(e) => set('sshKey', e.target.value)}
                  placeholder={existing?.hasSshKey ? 'Uendret' : '-----BEGIN OPENSSH PRIVATE KEY-----'}
                  rows={3}
                  spellCheck={false}
                />
                <span className="hint">Nøkkelen kan ikke ha passordfrase.</span>
              </label>
              {needsDir && (
                <label className="field">
                  <span className="field-label">Mappen med torrentfilene</span>
                  <input
                    type="text"
                    value={form.torrentDir}
                    onChange={(e) => set('torrentDir', e.target.value)}
                    placeholder={kind?.torrentDirHint}
                    spellCheck={false}
                  />
                  <span className="hint">
                    Relativt til hjemmemappen, eller full sti. For {kind?.label} er det vanligvis {kind?.torrentDirHint}.
                    Har du flere instanser på samme server, har hver sin mappe.
                  </span>
                </label>
              )}
              {storedFingerprint && !pinKey && <p className="hint">Lagret servernøkkel: {storedFingerprint}</p>}
              {pinKey?.replace && <p className="hint">Ny servernøkkel lagres når du lagrer: {pinKey.fingerprint}</p>}
            </details>
          )}

          <div className="dialog-status" aria-live="polite">
            {test?.ok && (
              <p className="status-ok">
                Koblet til {kind?.label} {test.version}.
              </p>
            )}
            {test && !test.ok && (
              <p className="status-bad">{connectionProblem(test.errorCode, kind?.label ?? '', test.error)}</p>
            )}
            {test?.ssh && sshStatus(test.ssh)}
            {error && <p className="status-bad">{error}</p>}
          </div>

          <div className="dialog-actions">
            {existing &&
              (confirmDelete ? (
                <button type="button" className="btn btn-danger" onClick={remove} disabled={busy !== null}>
                  Bekreft: fjern {existing.name}
                </button>
              ) : (
                <button type="button" className="btn btn-quiet" onClick={() => setConfirmDelete(true)}>
                  Fjern klient
                </button>
              ))}
            <span className="dialog-spacer" />
            <button type="button" className="btn" onClick={runTest} disabled={busy !== null || !form.url}>
              {busy === 'test' ? 'Tester …' : 'Test tilkobling'}
            </button>
            <button type="submit" className="btn btn-primary" disabled={busy !== null}>
              {existing ? 'Lagre klient' : 'Legg til klient'}
            </button>
          </div>
          {existing && confirmDelete && (
            <p className="hint">Dette fjerner bare klienten fra Stim. Torrentene i klienten blir ikke rørt.</p>
          )}
        </form>
        <button type="button" className="dialog-close" onClick={onClose} aria-label="Lukk">
          ×
        </button>
      </div>
    </div>
  );
}
