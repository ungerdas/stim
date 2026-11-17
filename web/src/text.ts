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
  copying: 'Kopierer filer',
  adding: 'Legger til',
  checking: 'Sjekker filer',
  removing: 'Fjerner fra kilden',
  moving: 'Flytter filer',
  done: 'Ferdig',
  failed: 'Feilet',
  cancelled: 'Avbrutt',
};

const transferProblems: Record<string, string> = {
  'needs-file-access': 'Klienten ga ikke fra seg torrentfilen. Sett opp SSH-tilgang under Endre på kildeklienten.',
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
  'ssh-not-ready': 'SSH-tilgang mangler eller er ikke testet og lagret. Sett den opp under Endre på klienten.',
  'source-file-missing': 'En fil mangler eller har feil størrelse på den gamle serveren. Ingenting er endret.',
  'no-space': 'Det er ikke nok ledig plass på den nye serveren. Ingenting er endret.',
  'copy-failed': 'Kopieringen feilet. Det som ble kopiert er slettet, og kilden er urørt.',
  'target-file-exists':
    'Det ligger allerede en annen fil med samme navn i mappen på den nye serveren. Stim skriver ikke over den. Velg en annen mappe.',
  'copy-interrupted':
    'Kopieringen stoppet underveis. Ferdige filer er beholdt på den nye serveren, så neste forsøk fortsetter der. Kilden er urørt.',
};

const copyProblems: Record<string, string> = {
  'data-not-found': 'Målklienten fant ikke alle de kopierte filene. Kopien er slettet, og kilden er urørt.',
  'host-key-changed': 'En servers SSH-nøkkel er endret siden sist. Sjekk den under Endre på klienten.',
};

/** Why a finished move left the files where they were, if it did. */
export function transferNote(item: TransferItem): string | null {
  switch (item.note) {
    case 'source-files-shared':
      return `Filene på den gamle serveren er beholdt fordi ${item.shared === 1 ? 'en annen torrent' : `${item.shared} andre torrenter`} der bruker dem.`;
    case 'source-files-unchecked':
      return 'Filene på den gamle serveren er beholdt: Stim fikk ikke sjekket om andre torrenter der bruker dem.';
    case 'source-files-same':
      return 'Filene på den gamle serveren er beholdt fordi målklienten bruker de samme filene.';
    case 'not-moved-shared':
      return `Filene er ikke flyttet til den nye mappen fordi ${item.shared === 1 ? 'en annen torrent' : `${item.shared} andre torrenter`} bruker dem. Målklienten seeder fra der de lå.`;
    case 'group-pending':
      return 'Venter på de andre torrentene med de samme filene.';
    case 'not-moved-group':
      return 'Filene er ikke flyttet til den nye mappen fordi ikke alle torrentene som bruker dem, ble flyttet. Målklienten seeder fra der de lå.';
    case 'kept-group':
      return 'Filene på den gamle serveren er beholdt fordi ikke alle torrentene som bruker dem, ble flyttet.';
    case 'not-moved-unchecked':
      return 'Filene er ikke flyttet til den nye mappen: Stim fikk ikke sjekket om andre torrenter bruker dem. Målklienten seeder fra der de lå.';
    default:
      return null;
  }
}

/** A failed transfer item in plain words, with the technical detail apart. */
export function transferProblem(item: TransferItem, copyFiles = false): { text: string; detail?: string } {
  const text = item.code ? (copyFiles && copyProblems[item.code]) || transferProblems[item.code] : undefined;
  if (text) return { text, detail: item.error };
  return { text: item.error || 'Ukjent feil.' };
}
