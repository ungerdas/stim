// One fish, drawn facing right around the origin and roughly two units long.
// Scale it with a transform. Hollow fish mean downloading, filled mean
// uploading; the same glyph labels the totals so it doubles as the legend.

export const FISH_BODY = 'M-0.5 0C-0.15-0.44 0.6-0.44 1 0C0.6 0.44-0.15 0.44-0.5 0Z';
export const FISH_TAIL = 'M-0.42 0L-1-0.36V0.36Z';

export function FishGlyph({ hollow }: { hollow?: boolean }) {
  return (
    <svg className="fish-glyph" viewBox="-1.15 -0.6 2.3 1.2" aria-hidden="true">
      <g
        fill={hollow ? 'none' : 'currentColor'}
        stroke="currentColor"
        strokeWidth={hollow ? 0.16 : 0.06}
        strokeLinejoin="round"
      >
        <path d={FISH_BODY} />
        <path d={FISH_TAIL} />
      </g>
    </svg>
  );
}
