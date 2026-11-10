const nb = 'nb-NO';

const oneDecimal = new Intl.NumberFormat(nb, { minimumFractionDigits: 1, maximumFractionDigits: 1 });
const twoDecimals = new Intl.NumberFormat(nb, { minimumFractionDigits: 2, maximumFractionDigits: 2 });
const whole = new Intl.NumberFormat(nb, { maximumFractionDigits: 0 });

const UNITS = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB'];

/** 1536 → "1,5 KiB". Binary units, as torrent clients report them. */
export function bytes(n: number): string {
  if (!Number.isFinite(n) || n <= 0) return '0 B';
  let i = 0;
  let v = n;
  while (v >= 1024 && i < UNITS.length - 1) {
    v /= 1024;
    i++;
  }
  const num = i === 0 ? whole.format(v) : v >= 100 ? whole.format(v) : oneDecimal.format(v);
  return `${num} ${UNITS[i]}`;
}

/** Transfer rate, or an empty string when nothing is moving. */
export function rate(n: number): string {
  return n > 0 ? `${bytes(n)}/s` : '';
}

export function count(n: number): string {
  return whole.format(n);
}

export function ratio(n: number): string {
  if (n < 0 || !Number.isFinite(n)) return '–';
  return twoDecimals.format(n);
}

export function percent(progress: number): string {
  const p = Math.max(0, Math.min(1, progress)) * 100;
  // Never round an unfinished torrent up to 100 %.
  if (p >= 100) return '100 %';
  return `${oneDecimal.format(Math.min(p, 99.9))} %`;
}

/** Remaining time, coarse on purpose: "3 t 12 min", "2 d 4 t". */
export function eta(seconds: number): string {
  if (seconds < 0) return '∞';
  if (seconds < 60) return `${Math.max(1, Math.round(seconds))} s`;
  const m = Math.floor(seconds / 60);
  if (m < 60) return `${m} min`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h} t ${m % 60} min`;
  const d = Math.floor(h / 24);
  if (d > 365) return '∞';
  return `${d} d ${h % 24} t`;
}

const sameYear = new Intl.DateTimeFormat(nb, { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' });
const otherYear = new Intl.DateTimeFormat(nb, { day: 'numeric', month: 'short', year: 'numeric' });

export function date(unix: number): string {
  if (!unix) return '';
  const d = new Date(unix * 1000);
  return d.getFullYear() === new Date().getFullYear() ? sameYear.format(d) : otherYear.format(d);
}
