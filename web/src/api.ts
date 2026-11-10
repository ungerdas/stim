import type {
  InstanceConfig,
  InstanceInput,
  KindInfo,
  Snapshot,
  TestResult,
  TransferJob,
  TransferRequest,
} from './types';

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    ...init,
    headers: init?.body ? { 'Content-Type': 'application/json' } : undefined,
  });
  if (res.status === 204) return undefined as T;
  const data = await res.json().catch(() => null);
  if (!res.ok) {
    throw new Error(data?.error ?? `HTTP ${res.status}`);
  }
  return data as T;
}

const json = (method: string, body: unknown): RequestInit => ({ method, body: JSON.stringify(body) });

export const api = {
  kinds: () => request<KindInfo[]>('/api/kinds'),
  torrents: (signal?: AbortSignal) => request<Snapshot>('/api/torrents', { signal }),
  instances: () => request<InstanceConfig[]>('/api/instances'),
  createInstance: (input: InstanceInput) => request<InstanceConfig>('/api/instances', json('POST', input)),
  updateInstance: (id: string, input: InstanceInput) =>
    request<InstanceConfig>(`/api/instances/${id}`, json('PUT', input)),
  deleteInstance: (id: string) => request<void>(`/api/instances/${id}`, { method: 'DELETE' }),
  retryInstance: (id: string) => request<void>(`/api/instances/${id}/retry`, json('POST', {})),
  testInstance: (input: InstanceInput) => request<TestResult>('/api/instances/test', json('POST', input)),
  act: (action: 'pause' | 'resume', items: { instanceId: string; hash: string }[]) =>
    request<{ failed: Record<string, string> }>('/api/torrents/action', json('POST', { action, items })),
  remove: (items: { instanceId: string; hash: string }[], deleteData: boolean) =>
    request<{ failed: Record<string, string> }>('/api/torrents/remove', json('POST', { items, deleteData })),
  transfers: () => request<TransferJob[]>('/api/transfers'),
  startTransfer: (req: TransferRequest) => request<TransferJob>('/api/transfers', json('POST', req)),
  cancelTransfer: (id: string) => request<void>(`/api/transfers/${id}/cancel`, json('POST', {})),
};
