import type { CSSProperties } from 'react'
import { pixelColors, portraitPixels, spritePixels, type SpriteDefinition } from './pixel-sprites'
export interface PixelSpriteProps {
  sprite: SpriteDefinition
  size?: number
  frame?: number
  label?: string
}
const spriteStyle = (sprite: SpriteDefinition): CSSProperties =>
  ({
    '--tone': sprite.tone,
    '--sprite-tone': sprite.tone,
    '--sprite-hair': sprite.hair ?? sprite.tone,
    '--sprite-highlight':
      sprite.highlight ?? 'color-mix(in srgb, var(--sprite-tone) 55%, var(--surface))',
  }) as CSSProperties
function Pixels({
  sprite,
  size = 96,
  frame = 0,
  label,
  portrait,
}: PixelSpriteProps & { portrait: boolean }) {
  const pixels = portrait ? portraitPixels(sprite) : spritePixels(sprite, frame)
  return (
    <span
      className={`pixel-avatar pixel-art ${portrait ? 'pixel-portrait' : 'pixel-sprite'}`}
      role={label ? 'img' : undefined}
      aria-label={label}
      style={{ ...spriteStyle(sprite), width: portrait ? size : (size * 16) / 28, height: size }}
    >
      <svg
        viewBox={`0 0 ${portrait ? 14 : 16} ${portrait ? 14 : 28}`}
        shapeRendering="crispEdges"
        aria-hidden="true"
      >
        {pixels.flatMap((row, y) =>
          row.map((pixel, x) =>
            pixel === '.' ? null : (
              <rect key={`${x}-${y}`} x={x} y={y} width={1} height={1} fill={pixelColors[pixel]} />
            ),
          ),
        )}
      </svg>
    </span>
  )
}
/** Renders any supplied variant and palette as a world sprite; size is height in CSS pixels, frame selects a pose. */
export function PixelSprite(props: PixelSpriteProps) {
  return <Pixels {...props} portrait={false} />
}
/** Renders a supplied variant and palette as a portrait; size is its square size and label names it accessibly. */
export function PixelPortrait(props: PixelSpriteProps) {
  return <Pixels {...props} portrait />
}
