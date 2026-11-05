import type { AvatarSpec } from './avatar-spec'
import { getPortraitPixels, type PortraitVariant } from './avatar-pixels'
export type SpriteVariant = PortraitVariant
export interface SpriteDefinition {
  variant: SpriteVariant
  tone: string
  hair?: string
  highlight?: string
  scale?: number
  skin?: string
  outfit?: string
  accent?: string
  accessory?: AvatarSpec['accessory']
  background?: string
}
export function portraitPixels(sprite: SpriteDefinition) {
  const grid = getPortraitPixels(sprite.variant)
  if (sprite.skin && sprite.variant !== 'hood' && sprite.variant !== 'cap') {
    // The shared silhouettes use hood shading on the head; personal avatars
    // give uncovered heads their selected hair palette instead.
    for (let y = 0; y < 10; y++) {
      grid[y] = grid[y]!.map((pixel) => (pixel === 'h' || pixel === 'l' ? 'a' : pixel))
    }
  }
  return overlayAccessory(grid, sprite.accessory, true)
}
/** A shared portrait silhouette extends into a detailed 16 × 28 body; six precomputed poses. */
export function spritePixels(sprite: SpriteDefinition, frame = 0, action = 'idle'): string[][] {
  const grid = Array.from({ length: 28 }, () => Array<string>(16).fill('.'))
  const portrait = portraitPixels({ ...sprite, accessory: undefined })
  portrait.slice(0, 11).forEach((row, y) =>
    row.forEach((pixel, x) => {
      grid[y]![x + 1] = pixel
    }),
  )
  const fill = (x: number, y: number, w: number, h: number, color: string) => {
    for (let j = y; j < y + h; j++) for (let i = x; i < x + w; i++) grid[j]![i] = color
  }
  fill(4, 11, 8, 11, 'o')
  fill(5, 11, 6, 10, 't')
  fill(3, 12, 2, 7, 'h')
  fill(11, 12, 2, 7, 'h')
  fill(3, 18, 2, 2, 's')
  fill(11, 18, 2, 2, 's')
  fill(5, 19, 6, 1, 'g')
  fill(7, 12, 2, 1, 'g')
  const stride = frame >= 2 ? (frame % 2 ? 1 : -1) : 0
  fill(5 + stride, 22, 2, 4, 'o')
  fill(9 - stride, 22, 2, 4, 'o')
  fill(4 + stride, 26, 3, 1, 'o')
  fill(9 - stride, 26, 3, 1, 'o')
  if (sprite.variant !== 'hood') {
    fill(5, 4, 6, 5, 's')
    fill(6, 6, 1, 1, 'o')
    fill(9, 6, 1, 1, 'o')
    fill(7, 8, 2, 1, 'g')
  }
  if (sprite.variant === 'long-hair' || sprite.variant === 'bow') {
    fill(3, 4, 2, 14, 'a')
    fill(11, 4, 2, 14, 'a')
    fill(2, 12, 1, 6, 'a')
  }
  if (sprite.variant === 'cap') {
    fill(3, 1, 10, 3, 'h')
    fill(4, 1, 7, 1, 'l')
    fill(10, 4, 5, 1, 'h')
    fill(10, 1, 1, 1, 'g')
  }
  if (sprite.variant === 'bow') {
    fill(11, 1, 3, 2, 'g')
    fill(10, 3, 2, 2, 'g')
  }
  if (sprite.variant === 'beard') {
    fill(5, 8, 6, 2, 'a')
    fill(6, 10, 4, 1, 'a')
    fill(7, 13, 2, 5, 'l')
  }
  if (sprite.variant === 'headphones') {
    fill(2, 4, 2, 5, 'g')
    fill(12, 4, 2, 5, 'g')
    fill(4, 0, 8, 1, 'g')
  }
  if (frame < 2) {
    if (action === 'read' || action === 'write') {
      fill(3, 15, 10, 5, 'g')
      fill(4, 15, 3, 4, 'l')
      fill(8, 15, 4, 4, 'l')
      fill(7, 15, 1, 5, 'o')
      if (frame === 1) fill(9, 16, 2, 1, 'o')
    }
    if (action === 'type') {
      fill(3, 17, 10, 2, 'o')
      fill(4, 17, 8, 1, 'l')
      fill(frame === 1 ? 5 : 9, 16, 2, 1, 's')
    }
    if (action === 'carry') {
      fill(4, 15, 8, 6, 'g')
      fill(5, 16, 6, 4, 'l')
    }
    if (action === 'think' && frame === 1) {
      fill(11, 10, 2, 3, 's')
      fill(11, 13, 2, 5, 'h')
    }
  }
  if (frame === 1) {
    grid[18]![3] = 'g'
    grid[18]![12] = 'g'
  }
  return overlayAccessory(grid, sprite.accessory, false)
}
export const pixelColors: Record<string, string> = {
  o: 'var(--pixel-dark)',
  h: 'color-mix(in srgb, var(--sprite-tone) 65%, var(--pixel-dark))',
  l: 'var(--sprite-highlight)',
  f: 'var(--pixel-face)',
  s: 'var(--pixel-skin)',
  e: 'var(--pixel-eyes)',
  t: 'var(--sprite-tone)',
  a: 'var(--sprite-hair)',
  g: 'var(--pixel-gold)',
}

/** Overlay coordinates share the face landmarks of the 14 × 14 portrait.
 * Full sprites offset those landmarks by one column and wear scarves lower. */
function overlayAccessory(
  grid: string[][],
  accessory: SpriteDefinition['accessory'],
  portrait: boolean,
): string[][] {
  const dx = portrait ? 0 : 1
  const fill = (x: number, y: number, width: number, height: number, color: string) => {
    for (let row = y; row < y + height; row++)
      for (let col = x; col < x + width; col++) grid[row]![col + dx] = color
  }
  switch (accessory) {
    case 'glasses':
      // Leave skin beside each eye so the frames read as clear lenses.
      fill(3, 5, 8, 1, 'o')
      fill(3, 7, 8, 1, 'o')
      for (const x of [3, 6, 7, 10]) fill(x, 6, 1, 1, 'o')
      break
    case 'sunglasses':
      fill(4, 5, 6, 3, 'o')
      fill(5, 5, 1, 1, 'l')
      fill(8, 5, 1, 1, 'l')
      break
    case 'earring':
      fill(11, 8, 1, 2, 'g')
      fill(10, 9, 1, 1, 'l')
      break
    case 'scarf': {
      const y = portrait ? 10 : 11
      fill(4, y, 6, 2, 'g')
      fill(8, y + 2, 2, portrait ? 2 : 5, 'g')
      fill(4, y, 5, 1, 'l')
      break
    }
    case 'star': {
      const y = portrait ? 10 : 13
      fill(10, y, 1, 3, 'g')
      fill(9, y + 1, 3, 1, 'g')
      fill(10, y + 1, 1, 1, 'l')
      break
    }
  }
  return grid
}
