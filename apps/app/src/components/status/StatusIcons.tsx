import Svg, { Path, Rect } from 'react-native-svg';

/**
 * Fixed status colours, outside the theme tokens: a solid red
 * stop sign for "out" and critical items, a yellow hazard for "doubtful".
 * White on the red is 4.9:1; the dark ink on the yellow is 11:1.
 */
export const STATUS_COLORS = {
  stop: '#DA291C',
  onStop: '#FFFFFF',
  hazard: '#FFC21A',
  onHazard: '#1A1204',
} as const;

/** A white octagon with an exclamation mark, drawn on the red stop colour. */
export function StopIcon({ size = 16 }: { size?: number }) {
  return (
    <Svg width={size} height={size} viewBox="0 0 24 24" aria-hidden>
      <Path d="M8 2h8l6 6v8l-6 6H8l-6-6V8z" fill={STATUS_COLORS.onStop} />
      <Rect x={10.8} y={6} width={2.4} height={8} rx={1} fill={STATUS_COLORS.stop} />
      <Rect x={10.8} y={16} width={2.4} height={2.4} rx={1} fill={STATUS_COLORS.stop} />
    </Svg>
  );
}

/** A dark warning triangle, drawn on the yellow hazard colour. */
export function HazardIcon({ size = 14 }: { size?: number }) {
  return (
    <Svg width={size} height={(size * 22) / 24} viewBox="0 0 24 22" aria-hidden>
      <Path d="M12 1 23 21H1z" fill={STATUS_COLORS.onHazard} />
      <Rect x={10.8} y={7} width={2.4} height={8} rx={1} fill={STATUS_COLORS.hazard} />
      <Rect x={10.8} y={16.6} width={2.4} height={2.4} rx={1} fill={STATUS_COLORS.hazard} />
    </Svg>
  );
}
