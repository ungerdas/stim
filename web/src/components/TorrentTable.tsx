import { memo, useEffect, useRef, useState } from 'react';
import { useVirtualizer } from '@tanstack/react-virtual';
import type { InstanceStatus, Torrent } from '../types';
import { bytes, date, eta, percent, rate, ratio } from '../format';
import { stateLabel } from '../text';

export type SortKey =
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

export interface Sort {
  key: SortKey;
  dir: 'asc' | 'desc';
}

interface Column {
  key: SortKey;
  title: string;
  className: string;
  numeric?: boolean;
}

const COLUMNS: Column[] = [
  { key: 'name', title: 'Navn', className: 'col-name' },
  { key: 'instance', title: 'Klient', className: 'col-instance' },
  { key: 'state', title: 'Status', className: 'col-state' },
  { key: 'progress', title: 'Fremdrift', className: 'col-progress' },
  { key: 'size', title: 'Størrelse', className: 'col-size', numeric: true },
  { key: 'downloadRate', title: 'Ned', className: 'col-down', numeric: true },
  { key: 'uploadRate', title: 'Opp', className: 'col-up', numeric: true },
  { key: 'uploaded', title: 'Lastet opp', className: 'col-uploaded', numeric: true },
  { key: 'ratio', title: 'Ratio', className: 'col-ratio', numeric: true },
  { key: 'eta', title: 'Gjenstår', className: 'col-eta', numeric: true },
  { key: 'label', title: 'Etikett', className: 'col-label' },
  { key: 'tracker', title: 'Tracker', className: 'col-tracker' },
  { key: 'addedAt', title: 'Lagt til', className: 'col-added', numeric: true },
];

const HEADER_HEIGHT = 36;
const ROW_HEIGHT = 34;
const ROW_HEIGHT_COMPACT = 58;
const COMPACT_BELOW = 764; // keep in step with the narrowest @container tier in styles.css

export const torrentKey = (t: { instanceId: string; hash: string }) => `${t.instanceId}:${t.hash}`;

interface Props {
  torrents: Torrent[];
  instanceOf: (instanceId: string) => InstanceStatus | undefined;
  colorOf: (instanceId: string) => string;
  selected: Set<string>;
  onToggle: (key: string, range: boolean) => void;
  onToggleAll: () => void;
  sort: Sort;
  onSort: (key: SortKey) => void;
}

export function TorrentTable(props: Props) {
  const { torrents, selected, sort } = props;
  const scrollRef = useRef<HTMLDivElement>(null);
  const [compact, setCompact] = useState(false);

  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    const ro = new ResizeObserver(([entry]) => setCompact(entry.contentRect.width < COMPACT_BELOW));
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  const rowHeight = compact ? ROW_HEIGHT_COMPACT : ROW_HEIGHT;
  const virtualizer = useVirtualizer({
    count: torrents.length,
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
    <div className="table" ref={scrollRef} role="grid" aria-rowcount={torrents.length + 1}>
      <div className="thead trow" role="row">
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
        {COLUMNS.map((c) => {
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
            </div>
          );
        })}
      </div>

      <div className="tbody" style={{ height: virtualizer.getTotalSize() }}>
        {virtualizer.getVirtualItems().map((v) => {
          const t = torrents[v.index];
          const key = torrentKey(t);
          return (
            <Row
              key={key}
              torrent={t}
              rowKey={key}
              index={v.index}
              top={v.start - HEADER_HEIGHT}
              height={rowHeight}
              instanceName={props.instanceOf(t.instanceId)?.name ?? ''}
              color={props.colorOf(t.instanceId)}
              selected={selected.has(key)}
              onToggle={props.onToggle}
            />
          );
        })}
      </div>
    </div>
  );
}

interface RowProps {
  torrent: Torrent;
  rowKey: string;
  index: number;
  top: number;
  height: number;
  instanceName: string;
  color: string;
  selected: boolean;
  onToggle: (key: string, range: boolean) => void;
}

const Row = memo(function Row({ torrent: t, rowKey, index, top, height, instanceName, color, selected, onToggle }: RowProps) {
  const done = t.progress >= 1;
  const remaining = done || t.state !== 'downloading' ? '' : eta(t.eta);
  const stateText = stateLabel[t.state];
  const stateTitle = t.message ? `${t.rawState}: ${t.message}` : `Klientens status: ${t.rawState}`;
  const labelText = [t.label, ...t.tags].filter(Boolean).join(', ');

  return (
    <div
      className={`trow${selected ? ' is-selected' : ''}`}
      role="row"
      aria-rowindex={index + 2}
      aria-selected={selected}
      style={{ transform: `translateY(${top}px)`, height }}
      onClick={(e) => {
        // Let text selection and the checkbox itself work normally.
        if ((e.target as HTMLElement).closest('input')) return;
        if (window.getSelection()?.toString()) return;
        onToggle(rowKey, e.shiftKey);
      }}
    >
      <div className="cell col-check" role="gridcell">
        <input
          type="checkbox"
          aria-label={`Velg ${t.name}`}
          checked={selected}
          onChange={(e) => onToggle(rowKey, (e.nativeEvent as MouseEvent).shiftKey)}
        />
      </div>
      <div className="cell col-name" role="gridcell">
        <span className="name" title={t.name}>
          {t.name}
        </span>
        {/* Shown instead of the other columns when the table is narrow. */}
        <span className="name-meta">
          <span className="inst-dot" style={{ background: color }} aria-hidden="true" />
          <span className="name-meta-inst">{instanceName}</span>
          <span className={`state-text state-${t.state}`}>{stateText}</span>
          <span>{percent(t.progress)}</span>
          {t.downloadRate > 0 && <span>↓ {rate(t.downloadRate)}</span>}
          {t.uploadRate > 0 && <span>↑ {rate(t.uploadRate)}</span>}
        </span>
      </div>
      <div className="cell col-instance" role="gridcell">
        <span className="inst-dot" style={{ background: color }} aria-hidden="true" />
        <span className="ellipsis">{instanceName}</span>
      </div>
      <div className="cell col-state" role="gridcell" title={stateTitle}>
        <span className={`state-dot state-${t.state}`} aria-hidden="true" />
        <span className="ellipsis">{stateText}</span>
      </div>
      <div className="cell col-progress" role="gridcell">
        <span className={`bar state-${t.state}`} aria-hidden="true">
          <span className="bar-fill" style={{ width: `${Math.max(0, Math.min(1, t.progress)) * 100}%` }} />
        </span>
        <span className="bar-text">{percent(t.progress)}</span>
      </div>
      <div className="cell col-size is-numeric" role="gridcell">
        {bytes(t.size)}
      </div>
      <div className="cell col-down is-numeric" role="gridcell">
        {rate(t.downloadRate)}
      </div>
      <div className="cell col-up is-numeric" role="gridcell">
        {rate(t.uploadRate)}
      </div>
      <div className="cell col-uploaded is-numeric" role="gridcell">
        {bytes(t.uploaded)}
      </div>
      <div className="cell col-ratio is-numeric" role="gridcell">
        {ratio(t.ratio)}
      </div>
      <div className="cell col-eta is-numeric" role="gridcell">
        {remaining}
      </div>
      <div className="cell col-label" role="gridcell" title={labelText}>
        <span className="ellipsis">{labelText}</span>
      </div>
      <div className="cell col-tracker" role="gridcell" title={t.tracker}>
        <span className="ellipsis">{t.tracker}</span>
      </div>
      <div className="cell col-added is-numeric" role="gridcell">
        {date(t.addedAt)}
      </div>
    </div>
  );
});
