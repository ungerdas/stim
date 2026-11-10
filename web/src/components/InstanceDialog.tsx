import { useEffect, useRef, useState } from 'react';
import { api } from '../api';
import type { InstanceConfig, InstanceInput, KindInfo, TestResult } from '../types';
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
}

export function InstanceDialog({ kinds, existing, onClose, onChanged }: Props) {
  const ref = useRef<HTMLDialogElement>(null);
  const [form, setForm] = useState<FormState>(() => ({
    kind: existing?.kind ?? kinds[0]?.kind ?? '',
    name: existing?.name ?? '',
    url: existing?.url ?? '',
    username: existing?.username ?? '',
    password: '',
    basicUser: existing?.basicUser ?? '',
    basicPass: '',
    tlsSkipVerify: existing?.tlsSkipVerify ?? false,
  }));
  const [busy, setBusy] = useState<'test' | 'save' | 'delete' | null>(null);
  const [test, setTest] = useState<TestResult | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [confirmDelete, setConfirmDelete] = useState(false);

  useEffect(() => {
    ref.current?.showModal();
  }, []);

  const kind = kinds.find((k) => k.kind === form.kind);
  const set = <K extends keyof FormState>(key: K, value: FormState[K]) => {
    setForm((f) => ({ ...f, [key]: value }));
    setTest(null);
    setError(null);
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
  });

  const runTest = async () => {
    setBusy('test');
    setTest(null);
    setError(null);
    try {
      setTest(await api.testInstance(input()));
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

  return (
    <dialog
      ref={ref}
      className="dialog"
      onClose={onClose}
      onClick={(e) => {
        if (e.target === ref.current) ref.current?.close(); // click on the backdrop
      }}
    >
      <form className="dialog-body" onSubmit={save}>
        <h2 className="dialog-title">{existing ? `Endre ${existing.name}` : 'Legg til klient'}</h2>

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

        <div className="dialog-status" aria-live="polite">
          {test?.ok && (
            <p className="status-ok">
              Koblet til {kind?.label} {test.version}.
            </p>
          )}
          {test && !test.ok && (
            <p className="status-bad">{connectionProblem(test.errorCode, kind?.label ?? '', test.error)}</p>
          )}
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
      <button type="button" className="dialog-close" onClick={() => ref.current?.close()} aria-label="Lukk">
        ×
      </button>
    </dialog>
  );
}
