import { useEffect, useMemo, useRef, useState } from 'react';
import type { Torrent } from '../types';
import { rate } from '../format';
import { FISH_BODY, FISH_TAIL } from './Fish';

const HEIGHT = 56;
const MAX_FISH = 110;
const PAD_X = 22;
const PAD_Y = 12;

/** Stable pseudo-random number in [0, 1) from part of an info hash. */
function hashUnit(hash: string, offset: number): number {
  const slice = hash.slice(offset, offset + 6) || '0';
  return parseInt(slice, 16) / 0x1000000;
}

/** Fish length in px: 10 KiB/s is the smallest, 100 MiB/s and up the largest. */
function fishLength(bytesPerSecond: number): number {
  const lo = Math.log10(10 * 1024);
  const hi = Math.log10(100 * 1024 * 1024);
  const t = (Math.log10(Math.max(bytesPerSecond, 1)) - lo) / (hi - lo);
  return 9 + 15 * Math.max(0, Math.min(1, t));
}

interface Props {
  torrents: Torrent[];
  colorOf: (instanceId: string) => string;
  onPick: (torrent: Torrent) => void;
}

/**
 * The shoal: every torrent that is moving data right now is one fish. Its
 * colour is the client it lives on, its size the transfer rate, and it is
 * hollow while downloading and filled while uploading.
 */
export function Shoal({ torrents, colorOf, onPick }: Props) {
  const ref = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState(0);

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const ro = new ResizeObserver(([entry]) => setWidth(entry.contentRect.width));
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  const fish = useMemo(() => {
    return torrents
      .filter((t) => t.downloadRate > 0 || t.uploadRate > 0)
      .sort((a, b) => Math.max(b.downloadRate, b.uploadRate) - Math.max(a.downloadRate, a.uploadRate))
      .slice(0, MAX_FISH)
      .reverse(); // draw the fastest (largest) last, on top
  }, [torrents]);

  const active = fish.length;

  return (
    <div
      className="shoal"
      ref={ref}
      role="img"
      aria-label={
        active === 0
          ? 'Ingen torrenter overfører data akkurat nå'
          : `${active} torrenter overfører data akkurat nå`
      }
    >
      {active === 0 ? (
        <p className="shoal-empty">Stille vann. Ingen torrenter overfører data akkurat nå.</p>
      ) : (
        <svg width={width} height={HEIGHT} viewBox={`0 0 ${Math.max(width, 1)} ${HEIGHT}`}>
          {fish.map((t) => {
            const uploading = t.uploadRate >= t.downloadRate;
            const len = fishLength(Math.max(t.downloadRate, t.uploadRate));
            const x = PAD_X + hashUnit(t.hash, 0) * Math.max(width - PAD_X * 2, 1);
            const y = PAD_Y + hashUnit(t.hash, 8) * (HEIGHT - PAD_Y * 2);
            const scale = len / 2;
            const color = colorOf(t.instanceId);
            return (
              <g
                key={`${t.instanceId}:${t.hash}`}
                transform={`translate(${x.toFixed(1)} ${y.toFixed(1)})`}
                className="shoal-fish"
                onClick={() => onPick(t)}
              >
                <title>
                  {`${t.name}\n↓ ${rate(t.downloadRate) || '0'}   ↑ ${rate(t.uploadRate) || '0'}`}
                </title>
                <g
                  className="shoal-swim"
                  style={{
                    animationDuration: `${(3.2 + hashUnit(t.hash, 14) * 3).toFixed(2)}s`,
                    animationDelay: `${(-hashUnit(t.hash, 20) * 6).toFixed(2)}s`,
                  }}
                >
                  <g
                    transform={`scale(${scale.toFixed(2)})`}
                    fill={uploading ? color : 'var(--sea-shallow)'}
                    stroke={color}
                    strokeWidth={uploading ? 0.05 : 0.17}
                    strokeLinejoin="round"
                  >
                    <path d={FISH_BODY} />
                    <path d={FISH_TAIL} />
                  </g>
                </g>
              </g>
            );
          })}
        </svg>
      )}
    </div>
  );
}
