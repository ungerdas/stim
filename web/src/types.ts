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
  torrentDirHint?: string;
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
}

export interface TransferJob {
  id: string;
  targetId: string;
  removeFromSource: boolean;
  newPath?: string;
  items: TransferItem[];
  createdAt: number;
  finishedAt: number;
  cancelling: boolean;
}

export interface TransferRequest {
  targetId: string;
  removeFromSource: boolean;
  newPath: string;
  items: { instanceId: string; hash: string; name: string }[];
}
