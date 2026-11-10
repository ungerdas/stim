import type { Torrent } from './types';

export const torrentKey = (t: { instanceId: string; hash: string }) => `${t.instanceId}:${t.hash}`;

export interface DuplicateInfo {
  /** Stable ID of the group: the smallest of the torrent's IDs. */
  group: string;
  /** How many clients have the torrent. */
  clients: number;
  /** The other clients that have it. */
  otherInstanceIds: string[];
}

export interface Duplicates {
  /** Per torrent (by torrentKey), only for torrents in more than one client. */
  byKey: Map<string, DuplicateInfo>;
  /** The copies of each duplicated torrent, by group ID. */
  groups: Map<string, Torrent[]>;
}

/**
 * Finds torrents that are in more than one client at once. Copies are
 * matched by info hash, also when the clients use different ID forms for a
 * hybrid torrent (see Torrent.altHash).
 */
export function findDuplicates(torrents: Torrent[]): Duplicates {
  const parent = new Map<string, string>();
  const find = (x: string): string => {
    let root = x;
    while (parent.get(root) !== root) root = parent.get(root)!;
    // Path compression keeps later lookups short.
    while (parent.get(x) !== root) {
      const next = parent.get(x)!;
      parent.set(x, root);
      x = next;
    }
    return root;
  };
  const add = (x: string) => {
    if (!parent.has(x)) parent.set(x, x);
  };
  const union = (a: string, b: string) => {
    add(a);
    add(b);
    const ra = find(a);
    const rb = find(b);
    // The smaller ID becomes the root, so group IDs do not depend on order.
    if (ra !== rb) {
      if (ra < rb) parent.set(rb, ra);
      else parent.set(ra, rb);
    }
  };
  for (const t of torrents) {
    add(t.hash);
    if (t.altHash) union(t.hash, t.altHash);
  }

  const all = new Map<string, Torrent[]>();
  for (const t of torrents) {
    const g = find(t.hash);
    const list = all.get(g);
    if (list) list.push(t);
    else all.set(g, [t]);
  }

  const byKey = new Map<string, DuplicateInfo>();
  const groups = new Map<string, Torrent[]>();
  for (const [group, list] of all) {
    const clients = new Set(list.map((t) => t.instanceId));
    if (clients.size < 2) continue;
    groups.set(group, list);
    for (const t of list) {
      byKey.set(torrentKey(t), {
        group,
        clients: clients.size,
        otherInstanceIds: [...clients].filter((id) => id !== t.instanceId),
      });
    }
  }
  return { byKey, groups };
}
