import type { ErrorCode, InstanceStatus, State } from './types';

// All user-facing copy lives here, so the UI can be translated in one place.

export const stateLabel: Record<State, string> = {
  downloading: 'Laster ned',
  seeding: 'Seeder',
  paused: 'Pauset',
  queued: 'I kø',
  checking: 'Sjekker',
  moving: 'Flytter',
  error: 'Feil',
  unknown: 'Ukjent',
};

/** A connection problem in plain words. Falls back to the client's own message. */
export function connectionProblem(code: ErrorCode | undefined, kindLabel: string, raw?: string): string {
  switch (code) {
    case 'auth':
      return 'Klienten godtok ikke brukernavnet eller passordet.';
    case 'wrong-type':
      return `Adressen svarte ikke som ${kindLabel}. Sjekk URL-en og klienttypen.`;
    case 'unreachable':
      return 'Får ikke kontakt med klienten. Sjekk adressen og at klienten kjører.';
    default:
      return raw || 'Ukjent feil.';
  }
}

export function instanceProblemShort(inst: InstanceStatus): string {
  switch (inst.errorCode) {
    case 'auth':
      return 'Innlogging avvist';
    case 'wrong-type':
      return 'Feil adresse eller type';
    case 'unreachable':
      return 'Får ikke kontakt';
    default:
      return 'Feil';
  }
}
