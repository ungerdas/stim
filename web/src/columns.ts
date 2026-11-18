// The torrent list's columns: which are shown and how wide they are. The
// person can resize and hide columns; when the list is too narrow for what is
// left, the least urgent columns step aside instead of the list scrolling
// sideways.

export type ColumnKey =
  | 'name'
  | 'instance'
  | 'state'
  | 'progress'
  | 'size'
  | 'downloadRate'
  | 'uploadRate'
  | 'uploaded'
  | 'ratio'
  | 'eta'
  | 'label'
  | 'tracker'
  | 'addedAt';

export interface ColumnDef {
  key: ColumnKey;
  title: string;
  className: string;
  numeric?: boolean;
  /** Default width in pixels; for the name, the least it gets (it takes the space left over). */
  width: number;
  min: number;
  /** When space runs out, columns step aside in this order; without it, a column always stays. */
  drop?: number;
}

export const COLUMNS: ColumnDef[] = [
  { key: 'name', title: 'Navn', className: 'col-name', width: 220, min: 120 },
  { key: 'instance', title: 'Klient', className: 'col-instance', width: 150, min: 60 },
  { key: 'state', title: 'Status', className: 'col-state', width: 112, min: 60 },
  { key: 'progress', title: 'Fremdrift', className: 'col-progress', width: 120, min: 70 },
  { key: 'size', title: 'Størrelse', className: 'col-size', numeric: true, width: 84, min: 56, drop: 6 },
  { key: 'downloadRate', title: 'Ned', className: 'col-down', numeric: true, width: 92, min: 56 },
  { key: 'uploadRate', title: 'Opp', className: 'col-up', numeric: true, width: 92, min: 56 },
  { key: 'uploaded', title: 'Lastet opp', className: 'col-uploaded', numeric: true, width: 100, min: 56, drop: 4 },
  { key: 'ratio', title: 'Ratio', className: 'col-ratio', numeric: true, width: 56, min: 44, drop: 5 },
  { key: 'eta', title: 'Gjenstår', className: 'col-eta', numeric: true, width: 84, min: 56, drop: 2 },
  { key: 'label', title: 'Etikett', className: 'col-label', width: 124, min: 60, drop: 3 },
  { key: 'tracker', title: 'Tracker', className: 'col-tracker', width: 130, min: 60, drop: 7 },
  { key: 'addedAt', title: 'Lagt til', className: 'col-added', numeric: true, width: 118, min: 70, drop: 1 },
];

export const CHECK_WIDTH = 40;
export const MAX_WIDTH = 1600;

const BY_KEY = new Map(COLUMNS.map((c) => [c.key, c]));
export const columnDef = (key: ColumnKey) => BY_KEY.get(key)!;

/** What the person has chosen: widths they dragged to, and columns they turned off. */
export interface ColumnPrefs {
  widths: Partial<Record<ColumnKey, number>>;
  hidden: ColumnKey[];
}

export const DEFAULT_PREFS: ColumnPrefs = { widths: {}, hidden: [] };

const STORE_KEY = 'stim.columns';

export function loadColumnPrefs(): ColumnPrefs {
  try {
    const v: unknown = JSON.parse(localStorage.getItem(STORE_KEY) ?? 'null');
    if (!v || typeof v !== 'object') return DEFAULT_PREFS;
    const { widths, hidden } = v as Partial<ColumnPrefs>;
    const clean: ColumnPrefs = { widths: {}, hidden: [] };
    if (widths && typeof widths === 'object') {
      for (const [k, w] of Object.entries(widths)) {
        if (BY_KEY.has(k as ColumnKey) && typeof w === 'number' && Number.isFinite(w)) {
          clean.widths[k as ColumnKey] = w;
        }
      }
    }
    if (Array.isArray(hidden)) {
      clean.hidden = hidden.filter((k): k is ColumnKey => BY_KEY.has(k) && k !== 'name');
    }
    return clean;
  } catch {
    return DEFAULT_PREFS;
  }
}

export function saveColumnPrefs(prefs: ColumnPrefs) {
  try {
    localStorage.setItem(STORE_KEY, JSON.stringify(prefs));
  } catch {
    // Not remembered; the layout still applies until the page is reloaded.
  }
}

export const clampWidth = (key: ColumnKey, w: number) => Math.round(Math.min(MAX_WIDTH, Math.max(columnDef(key).min, w)));

export const widthOf = (prefs: ColumnPrefs, key: ColumnKey) =>
  clampWidth(key, prefs.widths[key] ?? columnDef(key).width);

export function setWidth(prefs: ColumnPrefs, key: ColumnKey, w: number | undefined): ColumnPrefs {
  const widths = { ...prefs.widths };
  if (w === undefined) delete widths[key];
  else widths[key] = clampWidth(key, w);
  return { ...prefs, widths };
}

export function setShown(prefs: ColumnPrefs, key: ColumnKey, shown: boolean): ColumnPrefs {
  if (key === 'name') return prefs;
  const hidden = prefs.hidden.filter((k) => k !== key);
  if (!shown) hidden.push(key);
  return { ...prefs, hidden };
}

export interface ColumnPlan {
  /** The columns drawn, in order. */
  shown: ColumnKey[];
  /** Turned on, but stepped aside because the list is too narrow. */
  dropped: ColumnKey[];
  /** grid-template-columns for every row, the checkbox column first. */
  template: string;
  /** The least width the row needs; wider than the list, it scrolls sideways. */
  total: number;
}

/** The least the name column is squeezed to when the list is narrow and the person hasn't sized it. */
const NAME_SQUEEZE_MIN = 160;

/**
 * Lays the columns out for a list `available` pixels wide (0: not measured
 * yet). When the columns don't fit, the least urgent ones step aside;
 * columns the person resized go last, and `pinned` (the one being dragged)
 * not at all, so a column never vanishes under the pointer. If it still
 * doesn't fit, the columns nobody resized give up some width before the list
 * scrolls sideways.
 */
export function planColumns(prefs: ColumnPrefs, available: number, pinned?: ColumnKey): ColumnPlan {
  let shown = COLUMNS.filter((c) => c.key === 'name' || !prefs.hidden.includes(c.key));
  const widths = new Map(shown.map((c) => [c.key, widthOf(prefs, c.key)]));
  const total = () => CHECK_WIDTH + shown.reduce((n, c) => n + widths.get(c.key)!, 0);
  const touched = (c: ColumnDef) => prefs.widths[c.key] !== undefined;
  const dropped: ColumnKey[] = [];
  if (available > 0) {
    const candidates = shown
      .filter((c) => c.drop !== undefined && c.key !== pinned)
      .sort((a, b) => Number(touched(a)) - Number(touched(b)) || a.drop! - b.drop!);
    for (const c of candidates) {
      if (total() <= available) break;
      shown = shown.filter((s) => s !== c);
      dropped.push(c.key);
    }
    const deficit = total() - available;
    if (deficit > 0) {
      const give = shown
        .filter((c) => !touched(c) && c.key !== pinned)
        .map((c) => ({ c, slack: widths.get(c.key)! - (c.key === 'name' ? NAME_SQUEEZE_MIN : c.min) }))
        .filter((g) => g.slack > 0);
      const slack = give.reduce((n, g) => n + g.slack, 0);
      const share = Math.min(1, deficit / slack);
      for (const g of give) widths.set(g.c.key, Math.floor(widths.get(g.c.key)! - g.slack * share));
    }
  }
  const tracks = shown.map((c) => {
    const w = widths.get(c.key)!;
    return c.key === 'name' ? `minmax(${w}px, 1fr)` : `${w}px`;
  });
  return {
    shown: shown.map((c) => c.key),
    dropped: COLUMNS.filter((c) => dropped.includes(c.key)).map((c) => c.key),
    template: [`${CHECK_WIDTH}px`, ...tracks].join(' '),
    total: total(),
  };
}
