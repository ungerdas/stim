import type { State, Torrent } from './types';
import { torrentKey } from './duplicates';

/**
 * The same release seeded as several torrents: on several trackers
 * (cross-seeding), or as copies in several clients. Recognised by name and
 * size, so it also covers copies on different servers.
 *
 * Cross-seed tools accept torrents that differ a little: one has an .nfo or
 * a sample the other lacks, or the tracker wrote the name in another case or
 * with spaces. So names are compared loosely and sizes within SIZE_TOLERANCE.
 */
export interface Release {
  key: string;
  members: Torrent[];
  /** The members added up, shaped as a torrent so it sorts like one. */
  summary: Torrent;
  trackers: string[];
  instanceIds: string[];
}

export type TableRow =
  | { type: 'torrent'; torrent: Torrent; child: boolean }
  | { type: 'release'; release: Release; open: boolean };

// cross-seed's own default for how far sizes may differ.
export const SIZE_TOLERANCE = 0.02;

/** Whether two sizes are close enough to be the same release. */
export const sameSize = (a: number, b: number) =>
  a > 0 && b > 0 && Math.abs(a - b) <= Math.max(a, b) * SIZE_TOLERANCE;

const VIDEO_EXTENSION = /\.(mkv|mp4|m4v|avi|ts|m2ts|wmv)$/i;

/** "Farmen S21 NORWEGiAN" and "farmen.s21.norwegian.mkv" compare equal. */
export const looseName = (name: string) =>
  name
    .toLowerCase()
    .replace(VIDEO_EXTENSION, '')
    .replace(/[\s._]+/g, '.')
    .replace(/^\.+|\.+$/g, '');

/**
 * Splits torrents into releases: same loose name, and sizes within
 * SIZE_TOLERANCE of the smallest in the release. Torrents with no size yet
 * (a magnet still fetching its metadata) stay on their own.
 */
function groupReleases(torrents: Torrent[]): Map<string, Torrent[]> {
  const byName = new Map<string, Torrent[]>();
  const out = new Map<string, Torrent[]>();
  for (const t of torrents) {
    if (t.size <= 0) {
      out.set(`single\u0000${torrentKey(t)}`, [t]);
      continue;
    }
    const n = looseName(t.name);
    const list = byName.get(n);
    if (list) list.push(t);
    else byName.set(n, [t]);
  }
  for (const [name, list] of byName) {
    const bySize = [...list].sort((a, b) => a.size - b.size);
    let start = 0;
    for (let i = 1; i <= bySize.length; i++) {
      const smallest = bySize[start].size;
      if (i < bySize.length && bySize[i].size - smallest <= bySize[i].size * SIZE_TOLERANCE) continue;
      // Members keep the list's order, so they show up sorted like the rest.
      const members = new Set(bySize.slice(start, i));
      out.set(`${name}\u0000${smallest}`, list.filter((t) => members.has(t)));
      start = i;
    }
  }
  return out;
}

/** Keys of the torrents a row stands for, in display order. */
export const rowKeys = (row: TableRow): string[] =>
  row.type === 'torrent' ? [torrentKey(row.torrent)] : row.release.members.map(torrentKey);

export const rowId = (row: TableRow) =>
  row.type === 'torrent' ? torrentKey(row.torrent) : `release:${row.release.key}`;

// The state shown for a release: anything that needs attention comes before
// plain seeding.
const STATE_ORDER: State[] = ['error', 'downloading', 'checking', 'moving', 'queued', 'paused', 'unknown', 'seeding'];

const uniqueSorted = (values: string[]) =>
  [...new Set(values.filter(Boolean))].sort((a, b) => a.localeCompare(b, 'nb'));

function trackerProblem(members: Torrent[]): Pick<Torrent, 'trackerStatus' | 'trackerMessage'> {
  for (const status of ['unregistered', 'error'] as const) {
    const hit = members.filter((t) => t.trackerStatus === status);
    if (hit.length > 0) {
      return { trackerStatus: status, trackerMessage: hit.map((t) => `${t.tracker}: ${t.trackerMessage ?? ''}`).join('; ') };
    }
  }
  return { trackerStatus: undefined, trackerMessage: undefined };
}

function summarize(key: string, members: Torrent[]): Release {
  const first = members[0];
  const size = Math.max(...members.map((t) => t.size));
  const trackers = uniqueSorted(members.map((t) => t.tracker));
  const instanceIds = [...new Set(members.map((t) => t.instanceId))];
  const uploaded = members.reduce((n, t) => n + t.uploaded, 0);
  const state = STATE_ORDER.find((s) => members.some((t) => t.state === s)) ?? first.state;
  const downloading = members.filter((t) => t.state === 'downloading' && t.eta >= 0);
  const added = members.map((t) => t.addedAt).filter((n) => n > 0);
  const summary: Torrent = {
    ...first,
    hash: `release:${key}`,
    altHash: undefined,
    instanceId: instanceIds.length === 1 ? instanceIds[0] : '',
    state,
    size,
    rawState: '',
    message: '',
    progress: Math.min(...members.map((t) => t.progress)),
    downloaded: members.reduce((n, t) => n + t.downloaded, 0),
    uploaded,
    // How many times the files have gone out, counting every tracker.
    ratio: size > 0 ? uploaded / size : 0,
    downloadRate: members.reduce((n, t) => n + t.downloadRate, 0),
    uploadRate: members.reduce((n, t) => n + t.uploadRate, 0),
    eta: downloading.length > 0 ? Math.max(...downloading.map((t) => t.eta)) : -1,
    seeds: members.reduce((n, t) => n + t.seeds, 0),
    peers: members.reduce((n, t) => n + t.peers, 0),
    label: uniqueSorted(members.map((t) => t.label)).join(', '),
    tags: uniqueSorted(members.flatMap((t) => t.tags)),
    tracker: trackers.join(', '),
    // The worst tracker problem among the members, naming where it is.
    ...trackerProblem(members),
    addedAt: added.length > 0 ? Math.min(...added) : 0,
  };
  return { key, members, summary, trackers, instanceIds };
}

/**
 * Turns the sorted list into table rows. With grouping, torrents of the same
 * release become one row, placed by its summary, with its members (in the
 * list's order) below it when open.
 */
export function buildRows(
  sorted: Torrent[],
  grouping: boolean,
  expanded: Set<string>,
  compare: (a: Torrent, b: Torrent) => number,
): TableRow[] {
  if (!grouping) return sorted.map((torrent) => ({ type: 'torrent', torrent, child: false }));

  const byKey = groupReleases(sorted);
  const top: { sortAs: Torrent; row: TableRow }[] = [];
  for (const [key, members] of byKey) {
    if (members.length === 1) {
      top.push({ sortAs: members[0], row: { type: 'torrent', torrent: members[0], child: false } });
    } else {
      const release = summarize(key, members);
      top.push({ sortAs: release.summary, row: { type: 'release', release, open: expanded.has(key) } });
    }
  }
  top.sort((a, b) => compare(a.sortAs, b.sortAs));

  const rows: TableRow[] = [];
  for (const { row } of top) {
    rows.push(row);
    if (row.type === 'release' && row.open) {
      for (const torrent of row.release.members) rows.push({ type: 'torrent', torrent, child: true });
    }
  }
  return rows;
}
