import type { ErrorCode, InstanceStatus, State, TransferItem, TransferState } from './types';

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

export const transferStateLabel: Record<TransferState, string> = {
  queued: 'Venter',
  fetching: 'Henter torrentfil',
  adding: 'Legger til',
  checking: 'Sjekker filer',
  removing: 'Fjerner fra kilden',
  moving: 'Flytter filer',
  done: 'Ferdig',
  failed: 'Feilet',
  cancelled: 'Avbrutt',
};

const transferProblems: Record<string, string> = {
  'needs-file-access': 'Klienten kan ikke gi fra seg torrentfilen. Sett opp SSH-tilgang under Endre på kildeklienten.',
  incomplete: 'Ikke ferdig nedlastet.',
  busy: 'Kildeklienten er opptatt med denne torrenten. Prøv igjen litt senere.',
  gone: 'Finnes ikke lenger i kildeklienten.',
  duplicate: 'Målklienten har den allerede.',
  'torrent-file-missing': 'Fant ikke torrentfilen i torrent-mappen på serveren. Sjekk mappen under Endre.',
  'torrent-file-mismatch': 'Torrentfilen på serveren hører til en annen torrent.',
  'no-trackers': 'Torrenten har ingen trackere.',
  'data-not-found':
    'Målklienten fant ikke alle filene. Begge klientene må se filene på samme sti. Ingenting er endret.',
  'source-not-removed':
    'Målklienten har tatt over, men torrenten ble ikke fjernet fra kilden. Fjern den der selv, uten å slette data.',
  'move-failed': 'Målklienten har tatt over, men filene ble ikke flyttet til den nye mappen.',
  'host-key-changed': 'Serverens SSH-nøkkel er endret siden sist. Sjekk den under Endre på kildeklienten.',
  'ssh-failed': 'SSH-tilkoblingen til serveren feilet.',
};

/** A failed transfer item in plain words, with the technical detail apart. */
export function transferProblem(item: TransferItem): { text: string; detail?: string } {
  const text = item.code ? transferProblems[item.code] : undefined;
  if (text) return { text, detail: item.error };
  return { text: item.error || 'Ukjent feil.' };
}
