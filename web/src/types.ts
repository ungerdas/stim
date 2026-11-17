export type State =
  | 'downloading'
  | 'seeding'
  | 'paused'
  | 'queued'
  | 'checking'
  | 'moving'
  | 'error'
  | 'unknown';

export interface Capabilities {
  labels: boolean;
  tags: boolean;
}

export interface Torrent {
  instanceId: string;
  hash: string;
  /** The torrent's other ID form, for hybrid torrents (see the backend). */
  altHash?: string;
  name: string;
  state: State;
  rawState: string;
  progress: number; // 0..1
  size: number;
  downloaded: number;
  uploaded: number;
  downloadRate: number;
  uploadRate: number;
  ratio: number; // -1 when undefined
  eta: number; // seconds, -1 when unknown
  seeds: number;
  peers: number;
  label: string;
  tags: string[];
  tracker: string;
  savePath: string;
  addedAt: number;
  completedAt: number;
  message?: string;
}

export type ErrorCode = 'auth' | 'wrong-type' | 'unreachable' | 'other';

export interface InstanceStatus {
  id: string;
  name: string;
  kind: string;
  capabilities: Capabilities;
  online: boolean;
  error?: string;
  errorCode?: ErrorCode;
  version?: string;
  updatedAt: number;
  count: number;
  downloadRate: number;
  uploadRate: number;
  /** Client-wide totals in bytes; -1 where the client does not report one. */
  totals?: {
    allTimeUpload: number;
    allTimeDownload: number;
    sessionUpload: number;
    sessionDownload: number;
  };
}

export interface Snapshot {
  instances: InstanceStatus[];
  torrents: Torrent[];
}

export interface KindInfo {
  kind: string;
  label: string;
  needsUsername: boolean;
  urlHint: string;
  capabilities: Capabilities;
  /** False when moving torrents away from this client needs SSH access. */
  exportsTorrentFiles: boolean;
  /** What SSH access is for: '' nothing, 'required' to move torrents away, 'optional' as a fallback. */
  ssh: '' | 'required' | 'optional';
  /** Usual folder of the .torrent files; empty when the client says where each one is. */
  torrentDirHint?: string;
  /** Whether the client can move a torrent's files itself. */
  canMoveFiles: boolean;
}

/** An instance's saved settings. Passwords never leave the server. */
export interface InstanceConfig {
  id: string;
  name: string;
  kind: string;
  url: string;
  username: string;
  hasPassword: boolean;
  basicUser: string;
  hasBasicPass: boolean;
  tlsSkipVerify: boolean;
  sshHost: string;
  sshPort: number;
  sshUser: string;
  hasSshPassword: boolean;
  hasSshKey: boolean;
  sshHostFingerprint: string;
  torrentDir: string;
  /** The machine the client runs on: the SSH host, or the host in its address. */
  server: string;
  /** SSH is set up and the server's key is stored, so files can be copied there. */
  sshReady: boolean;
}

/** What the form sends. Omitted passwords keep the stored value. */
export interface InstanceInput {
  id?: string;
  name: string;
  kind: string;
  url: string;
  username: string;
  password?: string;
  basicUser: string;
  basicPass?: string;
  tlsSkipVerify: boolean;
  sshHost: string;
  sshPort: number;
  sshUser: string;
  sshPassword?: string;
  sshKey?: string;
  torrentDir: string;
  /** The host key seen in a test, to be pinned when saving. */
  sshHostKey?: string;
  forgetHostKey?: boolean;
}

export interface SSHTestResult {
  ok: boolean;
  error?: string;
  torrents: number;
  hostKey?: string;
  fingerprint?: string;
  newHostKey?: boolean;
  hostKeyChanged?: boolean;
}

export interface TestResult {
  ok: boolean;
  version?: string;
  error?: string;
  errorCode?: ErrorCode;
  ssh?: SSHTestResult;
}

export type TransferState =
  | 'queued'
  | 'fetching'
  | 'copying'
  | 'adding'
  | 'checking'
  | 'removing'
  | 'moving'
  | 'done'
  | 'failed'
  | 'cancelled';

export interface TransferItem {
  sourceId: string;
  hash: string;
  name: string;
  state: TransferState;
  progress: number;
  code?: string;
  error?: string;
  inTarget: boolean;
  /** Copy progress between servers, in bytes; rate in bytes per second. */
  copyTotal?: number;
  copyDone?: number;
  copyRate?: number;
  /** Why the old files were kept or not moved, when that was asked for. */
  note?:
    | 'source-files-shared'
    | 'source-files-unchecked'
    | 'source-files-same'
    | 'not-moved-shared'
    | 'not-moved-unchecked'
    | 'group-pending'
    | 'not-moved-group'
    | 'kept-group';
  noteDetail?: string;
  shared?: number;
}

export interface TransferJob {
  id: string;
  targetId: string;
  removeFromSource: boolean;
  newPath?: string;
  copyFiles?: boolean;
  targetPath?: string;
  deleteSourceFiles?: boolean;
  items: TransferItem[];
  createdAt: number;
  finishedAt: number;
  cancelling: boolean;
}

export interface TransferRequest {
  targetId: string;
  removeFromSource: boolean;
  newPath: string;
  copyFiles: boolean;
  targetPath: string;
  deleteSourceFiles: boolean;
  items: { instanceId: string; hash: string; name: string }[];
}
