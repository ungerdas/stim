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
}

export interface TestResult {
  ok: boolean;
  version?: string;
  error?: string;
  errorCode?: ErrorCode;
}
