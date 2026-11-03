/** EXCALIBUR wordmark: spaced capitals whose "I" is a pixel sword (approved brand). */
const SWORD_I = [
  '..kk..',
  '.kbBk.',
  '.kbBk.',
  '.kbBk.',
  '.kbBk.',
  '.kbBk.',
  '.kbBk.',
  '.kbBk.',
  '.kbBk.',
  '.kbBk.',
  'kggggk',
  'kggggk',
  '..hh..',
  '..hh..',
  '..pp..',
  '......',
]

function SwordI({ height }: { height: number }) {
  const cell = height / SWORD_I.length
  const width = cell * SWORD_I[0]!.length
  const cls: Record<string, string> = {
    k: 'wm-k',
    b: 'wm-b',
    B: 'wm-B',
    g: 'wm-g',
    h: 'wm-h',
    p: 'wm-g',
  }
  return (
    <svg
      className="wordmark-sword"
      width={width}
      height={height}
      style={{ width, height }}
      viewBox={`0 0 ${width} ${height}`}
      shapeRendering="crispEdges"
      aria-hidden="true"
    >
      {SWORD_I.flatMap((row, y) =>
        [...row].map((ch, x) =>
          cls[ch] ? (
            <rect
              key={`${x}-${y}`}
              className={cls[ch]}
              x={x * cell}
              y={y * cell}
              width={cell + 0.02}
              height={cell + 0.02}
            />
          ) : null,
        ),
      )}
    </svg>
  )
}

export function Wordmark({ size = 19 }: { size?: number }) {
  return (
    <span className="wordmark" role="img" aria-label="Excalibur" style={{ fontSize: size }}>
      <span aria-hidden="true">EXCAL</span>
      <SwordI height={size * 1.42} />
      <span aria-hidden="true">BUR</span>
    </span>
  )
}
