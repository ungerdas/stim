import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { COLUMNS, DEFAULT_PREFS, setShown, type ColumnKey, type ColumnPrefs } from '../columns';

interface ChoicesProps {
  prefs: ColumnPrefs;
  /** Columns that are on but don't fit right now. */
  dropped?: ColumnKey[];
  onChange: (prefs: ColumnPrefs) => void;
}

/** A checkbox per column; the name is always shown. */
export function ColumnChoices({ prefs, dropped = [], onChange }: ChoicesProps) {
  return (
    <ul className="column-choices">
      {COLUMNS.filter((c) => c.key !== 'name').map((c) => {
        const on = !prefs.hidden.includes(c.key);
        const squeezed = on && dropped.includes(c.key);
        return (
          <li key={c.key}>
            <label className="column-choice">
              <input type="checkbox" checked={on} onChange={(e) => onChange(setShown(prefs, c.key, e.target.checked))} />
              <span>{c.title}</span>
              {squeezed && <span className="column-choice-note">får ikke plass nå</span>}
            </label>
          </li>
        );
      })}
    </ul>
  );
}

export const isDefaultLayout = (p: ColumnPrefs) => p.hidden.length === 0 && Object.keys(p.widths).length === 0;

interface MenuProps extends ChoicesProps {
  x: number;
  y: number;
  onFitName: () => void;
  onClose: () => void;
}

/** Opened by right-clicking the column headings. */
export function ColumnMenu({ x, y, prefs, dropped, onChange, onFitName, onClose }: MenuProps) {
  const ref = useRef<HTMLDivElement>(null);
  const [pos, setPos] = useState({ left: x, top: y });

  // Keep the whole menu on screen.
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const r = el.getBoundingClientRect();
    setPos({
      left: Math.max(8, Math.min(x, window.innerWidth - r.width - 8)),
      top: Math.max(8, Math.min(y, window.innerHeight - r.height - 8)),
    });
    el.querySelector<HTMLInputElement>('input')?.focus();
  }, [x, y]);

  useEffect(() => {
    const away = (e: Event) => {
      if (!ref.current?.contains(e.target as Node)) onClose();
    };
    const key = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    document.addEventListener('pointerdown', away, true);
    document.addEventListener('keydown', key);
    window.addEventListener('resize', onClose);
    window.addEventListener('blur', onClose);
    return () => {
      document.removeEventListener('pointerdown', away, true);
      document.removeEventListener('keydown', key);
      window.removeEventListener('resize', onClose);
      window.removeEventListener('blur', onClose);
    };
  }, [onClose]);

  return (
    <div ref={ref} className="column-menu" role="dialog" aria-label="Kolonner" style={pos}>
      <p className="column-menu-title">Kolonner</p>
      <ColumnChoices prefs={prefs} dropped={dropped} onChange={onChange} />
      <div className="column-menu-actions">
        <button
          type="button"
          className="btn btn-small"
          onClick={() => {
            onFitName();
            onClose();
          }}
        >
          Vis hele navnene
        </button>
        <button
          type="button"
          className="btn btn-small"
          disabled={isDefaultLayout(prefs)}
          onClick={() => onChange(DEFAULT_PREFS)}
        >
          Tilbakestill
        </button>
      </div>
    </div>
  );
}
