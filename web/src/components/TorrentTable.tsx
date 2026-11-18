import { memo, useCallback, useEffect, useMemo, useRef, useState, type CSSProperties } from 'react';
import { useVirtualizer } from '@tanstack/react-virtual';
import type { InstanceStatus, Torrent } from '../types';
import { bytes, date, eta, percent, rate, ratio } from '../format';
import { stateLabel } from '../text';
import { torrentKey } from '../duplicates';
import { rowId, rowKeys, type Release, type TableRow } from '../releases';
import {
  COLUMNS,
  columnDef,
  planColumns,
  setWidth,
  widthOf,
  type ColumnKey,
  type ColumnPrefs,
} from '../columns';
import { ColumnMenu } from './ColumnMenu';

export type SortKey = ColumnKey;

export interface Sort {
  key: SortKey;
  dir: 'asc' | 'desc';
}

const HEADER_HEIGHT = 36;
const ROW_HEIGHT = 34;
const ROW_HEIGHT_COMPACT = 58;
const COMPACT_AT = 764; // keep in step with the @container tier in styles.css
const COMPACT_STYLE = { '--cols': '40px minmax(0, 1fr)', '--table-min': '0px' } as CSSProperties;
const NO_COLUMNS: ReadonlySet<ColumnKey> = new Set();

export { torrentKey };

interface Props {
  /** The torrents in the list, for "select all". */
  torrents: Torrent[];
  /** What to draw: torrents, releases and the members of open releases. */
  rows: TableRow[];
  onExpand: (releaseKey: string) => void;
  /** Opens the detail panel for a torrent. */
  onOpen: (t: Torrent) => void;
  instanceOf: (instanceId: string) => InstanceStatus | undefined;
  colorOf: (instanceId: string) => string;
  selected: Set<string>;
  onToggle: (keys: string[], range: boolean) => void;
  onToggleAll: () => void;
  sort: Sort;
  onSort: (key: SortKey) => void;
  /** For torrents that are in more than one client: how many, and which others. */
  duplicateOf: (t: Torrent) => { clients: number; others: string } | undefined;
  columns: ColumnPrefs;
  onColumns: (prefs: ColumnPrefs) => void;
}

/** Text widths for fitting the name column, measured the way the list draws them. */
function textMeasurer(sample: Element | null) {
  const ctx = document.createElement('canvas').getContext('2d');
  const family = getComputedStyle(sample ?? document.body).fontFamily;
  return (text: string, weight: number | string, size: number) => {
    if (!ctx) return text.length * size * 0.6;
    ctx.font = `${weight} ${size}px ${family}`;
    return ctx.measureText(text).width;
  };
}

export function TorrentTable(props: Props) {
  const { torrents, rows, selected, sort, columns, onColumns } = props;
  const scrollRef = useRef<HTMLDivElement>(null);
  // The list's width inside its scrollbar (what the columns may fill), and
  // outside it (what the stylesheet's container query sees).
  const [size, setSize] = useState({ inner: 0, outer: 0 });
  const [dragging, setDragging] = useState<ColumnKey | null>(null);
  const [menu, setMenu] = useState<{ x: number; y: number } | null>(null);
  const closeMenu = useCallback(() => setMenu(null), []);
  const compact = size.outer > 0 && size.outer <= COMPACT_AT;
  const width = size.inner;

  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    const ro = new ResizeObserver(([entry]) =>
      setSize({ inner: entry.contentRect.width, outer: entry.borderBoxSize?.[0]?.inlineSize ?? el.offsetWidth }),
    );
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  const plan = useMemo(() => planColumns(columns, width, dragging ?? undefined), [columns, width, dragging]);
  const shownKey = compact ? '' : plan.shown.join(' ');
  const shown = useMemo<ReadonlySet<ColumnKey>>(
    () => (shownKey ? new Set(shownKey.split(' ') as ColumnKey[]) : NO_COLUMNS),
    [shownKey],
  );
  const tableStyle = compact
    ? COMPACT_STYLE
    : ({ '--cols': plan.template, '--table-min': `${plan.total}px` } as CSSProperties);

  /** Widens the name column to the longest name in the list, badges included. */
  const fitName = () => {
    const sample = scrollRef.current?.querySelector('.tbody .name') ?? null;
    const size = sample ? parseFloat(getComputedStyle(sample).fontSize) || 14 : 14;
    const measure = textMeasurer(sample);
    let widest = 0;
    for (const row of rows) {
      const t = row.type === 'torrent' ? row.torrent : row.release.summary;
      let w = 24 + measure(t.name, 500, size);
      if (row.type === 'release') {
        w += 16 + 7 + 7 + 14 + measure(releaseBadge(row.release).text, 400, 11);
      } else {
        if (row.child) w += 18;
        const dup = props.duplicateOf(t);
        if (dup && dup.clients > 1) w += 7 + 12 + measure(`${dup.clients}×`, 650, 11.5);
      }
      widest = Math.max(widest, w);
    }
    if (widest > 0) onColumns(setWidth(columns, 'name', Math.ceil(widest) + 4));
  };

  const resetWidth = (key: ColumnKey) => onColumns(setWidth(columns, key, undefined));

  const startResize = (e: React.PointerEvent<HTMLElement>, key: ColumnKey) => {
    if (e.button !== 0) return;
    e.preventDefault();
    const handle = e.currentTarget;
    const startX = e.clientX;
    const startW = handle.parentElement?.getBoundingClientRect().width ?? widthOf(columns, key);
    const startPrefs = columns;
    handle.setPointerCapture(e.pointerId);
    setDragging(key);
    document.body.classList.add('is-resizing');
    const move = (ev: PointerEvent) => onColumns(setWidth(startPrefs, key, startW + ev.clientX - startX));
    const end = () => {
      handle.removeEventListener('pointermove', move);
      handle.removeEventListener('pointerup', end);
      handle.removeEventListener('pointercancel', end);
      document.body.classList.remove('is-resizing');
      setDragging(null);
    };
    handle.addEventListener('pointermove', move);
    handle.addEventListener('pointerup', end);
    handle.addEventListener('pointercancel', end);
  };

  const resizeKey = (e: React.KeyboardEvent<HTMLElement>, key: ColumnKey) => {
    const step = e.shiftKey ? 64 : 16;
    const current = e.currentTarget.parentElement?.getBoundingClientRect().width ?? widthOf(columns, key);
    if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') {
      e.preventDefault();
      onColumns(setWidth(columns, key, current + (e.key === 'ArrowRight' ? step : -step)));
    } else if (e.key === 'Enter') {
      e.preventDefault();
      if (key === 'name') fitName();
      else resetWidth(key);
    }
  };

  const rowHeight = compact ? ROW_HEIGHT_COMPACT : ROW_HEIGHT;
  const virtualizer = useVirtualizer({
    count: rows.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => rowHeight,
    overscan: 14,
    scrollMargin: HEADER_HEIGHT,
  });
  // Row height changes when the layout switches between wide and compact.
  useEffect(() => virtualizer.measure(), [rowHeight, virtualizer]);

  const selectedHere = torrents.reduce((n, t) => n + (selected.has(torrentKey(t)) ? 1 : 0), 0);
  const allSelected = torrents.length > 0 && selectedHere === torrents.length;

  return (
    <div className="table" ref={scrollRef} role="grid" aria-rowcount={rows.length + 1} style={tableStyle}>
      <div
        className="thead trow"
        role="row"
        onContextMenu={
          compact
            ? undefined
            : (e) => {
                e.preventDefault();
                setMenu({ x: e.clientX, y: e.clientY });
              }
        }
      >
        <div className="cell col-check" role="columnheader">
          <input
            type="checkbox"
            aria-label="Velg alle i listen"
            checked={allSelected}
            ref={(el) => {
              if (el) el.indeterminate = selectedHere > 0 && !allSelected;
            }}
            onChange={props.onToggleAll}
            disabled={torrents.length === 0}
          />
        </div>
        {COLUMNS.filter((c) => c.key === 'name' || shown.has(c.key)).map((c) => {
          const active = sort.key === c.key;
          return (
            <div
              key={c.key}
              className={`cell ${c.className}${c.numeric ? ' is-numeric' : ''}`}
              role="columnheader"
              aria-sort={active ? (sort.dir === 'asc' ? 'ascending' : 'descending') : 'none'}
            >
              <button type="button" className="th-button" onClick={() => props.onSort(c.key)}>
                {c.title}
                <span className="th-arrow" aria-hidden="true">
                  {active ? (sort.dir === 'asc' ? '▲' : '▼') : ''}
                </span>
              </button>
              {!compact && (
                <span
                  className={`col-resizer${dragging === c.key ? ' is-dragging' : ''}`}
                  role="separator"
                  aria-orientation="vertical"
                  aria-label={`Bredden på ${c.title}`}
                  aria-valuenow={widthOf(columns, c.key)}
                  aria-valuemin={columnDef(c.key).min}
                  tabIndex={0}
                  title={
                    c.key === 'name'
                      ? 'Dra for å endre bredden. Dobbeltklikk for å få plass til det lengste navnet.'
                      : 'Dra for å endre bredden. Dobbeltklikk for standardbredden.'
                  }
                  onPointerDown={(e) => startResize(e, c.key)}
                  onDoubleClick={() => (c.key === 'name' ? fitName() : resetWidth(c.key))}
                  onKeyDown={(e) => resizeKey(e, c.key)}
                />
              )}
            </div>
          );
        })}
      </div>

      <div className="tbody" style={{ height: virtualizer.getTotalSize() }}>
        {virtualizer.getVirtualItems().map((v) => {
          const row = rows[v.index];
          const keys = rowKeys(row);
          const picked = keys.reduce((n, k) => n + (selected.has(k) ? 1 : 0), 0);
          const t = row.type === 'torrent' ? row.torrent : row.release.summary;
          const release = row.type === 'release' ? row.release : undefined;
          const ids = release ? release.instanceIds : [t.instanceId];
          const dup = row.type === 'torrent' ? props.duplicateOf(t) : undefined;
          return (
            <Row
              key={rowId(row)}
              dupClients={dup?.clients ?? 0}
              dupOthers={dup?.others ?? ''}
              torrent={t}
              keys={keys}
              release={release}
              open={row.type === 'release' && row.open}
              child={row.type === 'torrent' && row.child}
              onExpand={props.onExpand}
              onOpen={props.onOpen}
              index={v.index}
              top={v.start - HEADER_HEIGHT}
              height={rowHeight}
              instanceName={
                ids.length === 1 ? (props.instanceOf(ids[0])?.name ?? '') : `${ids.length} klienter`
              }
              color={props.colorOf(ids[0])}
              shown={shown}
              selected={picked > 0 && picked === keys.length}
              partial={picked > 0 && picked < keys.length}
              onToggle={props.onToggle}
            />
          );
        })}
      </div>

      {menu && (
        <ColumnMenu
          x={menu.x}
          y={menu.y}
          prefs={columns}
          dropped={plan.dropped}
          onChange={onColumns}
          onFitName={fitName}
          onClose={closeMenu}
        />
      )}
    </div>
  );
}

interface RowProps {
  torrent: Torrent;
  keys: string[];
  release?: Release;
  open: boolean;
  child: boolean;
  onExpand: (releaseKey: string) => void;
  onOpen: (t: Torrent) => void;
  partial: boolean;
  index: number;
  top: number;
  height: number;
  instanceName: string;
  color: string;
  /** The columns besides the name that are drawn. */
  shown: ReadonlySet<ColumnKey>;
  selected: boolean;
  onToggle: (keys: string[], range: boolean) => void;
  dupClients: number;
  dupOthers: string;
}

const TRACKER_PROBLEM = { unregistered: 'Avregistrert', error: 'Trackerfeil' } as const;

/** "norbits.net" → "norbits" */
const siteName = (domain: string) => (/^[\d.:]+$/.test(domain) ? domain : domain.split('.')[0] || domain);

/** "4 trackere", or "3 torrenter" when some share a tracker (copies in several clients). */
function releaseBadge(r: Release): { text: string; title: string } {
  const list = r.trackers.join(', ');
  if (r.trackers.length === r.members.length) {
    return { text: `${r.trackers.length} trackere`, title: `Seedes på ${list}` };
  }
  return { text: `${r.members.length} torrenter`, title: `${r.members.length} torrenter på ${list || 'ukjent tracker'}` };
}

const Row = memo(function Row({
  torrent: t,
  keys,
  release,
  open,
  child,
  onExpand,
  onOpen,
  partial,
  index,
  top,
  height,
  instanceName,
  color,
  shown,
  selected,
  onToggle,
  dupClients,
  dupOthers,
}: RowProps) {
  const done = t.progress >= 1;
  const remaining = done || t.state !== 'downloading' ? '' : eta(t.eta);
  const stateText = stateLabel[t.state];
  const stateTitle = t.message ? `${t.rawState}: ${t.message}` : `Klientens status: ${t.rawState}`;
  const labelText = [t.label, ...t.tags].filter(Boolean).join(', ');
  const badge = release && releaseBadge(release);
  const problemText = t.trackerStatus ? TRACKER_PROBLEM[t.trackerStatus] : '';
  const what = release ? `${t.name}, alle ${release.members.length}` : child ? `${t.name} på ${t.tracker || 'ukjent tracker'}` : t.name;

  return (
    <div
      className={`trow${selected ? ' is-selected' : ''}${release ? ' is-release' : ''}${child ? ' is-child' : ''}`}
      role="row"
      aria-rowindex={index + 2}
      aria-selected={selected}
      aria-expanded={release ? open : undefined}
      style={{ transform: `translateY(${top}px)`, height }}
      onClick={(e) => {
        // Let text selection, the checkbox and the expander work normally.
        if ((e.target as HTMLElement).closest('input, button')) return;
        if (window.getSelection()?.toString()) return;
        onToggle(keys, e.shiftKey);
      }}
    >
      <div className="cell col-check" role="gridcell">
        <input
          type="checkbox"
          aria-label={`Velg ${what}`}
          checked={selected}
          ref={(el) => {
            if (el) el.indeterminate = partial;
          }}
          onChange={(e) => onToggle(keys, (e.nativeEvent as MouseEvent).shiftKey)}
        />
      </div>
      <div className="cell col-name" role="gridcell">
        <span className="name-line">
          {release && (
            <button
              type="button"
              className="expander"
              aria-expanded={open}
              aria-label={open ? `Skjul de ${release.members.length} torrentene` : `Vis de ${release.members.length} torrentene`}
              onClick={() => onExpand(release.key)}
            >
              <span aria-hidden="true">{open ? '▾' : '▸'}</span>
            </button>
          )}
          <button
            type="button"
            className="name"
            title={release ? t.name : `${t.name}\nVis detaljer`}
            onClick={() => (release ? onExpand(release.key) : onOpen(t))}
          >
            {t.name}
          </button>
          {badge && (
            <span className="release-badge" title={badge.title}>
              {badge.text}
            </span>
          )}
          {dupClients > 1 && (
            <span className="dup-badge" title={`Seedes også fra ${dupOthers}`}>
              {dupClients}×
            </span>
          )}
        </span>
        {/* Shown instead of the other columns when the table is narrow. */}
        <span className="name-meta">
          {child && <span className="name-meta-tracker">{t.tracker || 'ukjent tracker'}</span>}
          <span className="inst-dot" style={{ background: color }} aria-hidden="true" />
          <span className="name-meta-inst">{instanceName}</span>
          <span className={`state-text state-${t.state}`}>{stateText}</span>
          <span>{percent(t.progress)}</span>
          {problemText && <span className={`tracker-${t.trackerStatus}`}>{problemText}</span>}
          {t.downloadRate > 0 && <span>↓ {rate(t.downloadRate)}</span>}
          {t.uploadRate > 0 && <span>↑ {rate(t.uploadRate)}</span>}
        </span>
      </div>
      {shown.has('instance') && (
        <div className="cell col-instance" role="gridcell">
          <span className="inst-dot" style={{ background: color }} aria-hidden="true" />
          <span className="ellipsis">{instanceName}</span>
        </div>
      )}
      {shown.has('state') && (
        <div className="cell col-state" role="gridcell" title={stateTitle}>
          <span className={`state-dot state-${t.state}`} aria-hidden="true" />
          <span className="ellipsis">{stateText}</span>
        </div>
      )}
      {shown.has('progress') && (
        <div className="cell col-progress" role="gridcell">
          <span className={`bar state-${t.state}`} aria-hidden="true">
            <span className="bar-fill" style={{ width: `${Math.max(0, Math.min(1, t.progress)) * 100}%` }} />
          </span>
          <span className="bar-text">{percent(t.progress)}</span>
        </div>
      )}
      {shown.has('size') && (
        <div className="cell col-size is-numeric" role="gridcell">
          {bytes(t.size)}
        </div>
      )}
      {shown.has('downloadRate') && (
        <div className="cell col-down is-numeric" role="gridcell">
          {rate(t.downloadRate)}
        </div>
      )}
      {shown.has('uploadRate') && (
        <div className="cell col-up is-numeric" role="gridcell">
          {rate(t.uploadRate)}
        </div>
      )}
      {shown.has('uploaded') && (
        <div className="cell col-uploaded is-numeric" role="gridcell">
          {bytes(t.uploaded)}
        </div>
      )}
      {shown.has('ratio') && (
        <div
          className="cell col-ratio is-numeric"
          role="gridcell"
          title={release ? 'Lastet opp på alle trackerne til sammen, delt på størrelsen' : undefined}
        >
          {ratio(t.ratio)}
        </div>
      )}
      {shown.has('eta') && (
        <div className="cell col-eta is-numeric" role="gridcell">
          {remaining}
        </div>
      )}
      {shown.has('label') && (
        <div className="cell col-label" role="gridcell" title={labelText}>
          <span className="ellipsis">{labelText}</span>
        </div>
      )}
      {shown.has('tracker') && (
        <div
          className={`cell col-tracker${t.trackerStatus ? ` tracker-${t.trackerStatus}` : ''}`}
          role="gridcell"
          title={problemText ? `${problemText}\n${t.trackerMessage ?? ''}` : t.tracker}
        >
          {problemText && (
            <span className="tracker-flag" role="img" aria-label={problemText}>
              !
            </span>
          )}
          {/* For a release, the site names alone, so more of them fit. */}
          <span className="ellipsis">{release ? release.trackers.map(siteName).join(', ') : t.tracker}</span>
        </div>
      )}
      {shown.has('addedAt') && (
        <div className="cell col-added is-numeric" role="gridcell">
          {date(t.addedAt)}
        </div>
      )}
    </div>
  );
});
