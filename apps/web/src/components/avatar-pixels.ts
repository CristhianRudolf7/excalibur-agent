import type { AgentId } from '@/features/shell/mock-data'

export type PortraitVariant = 'hood' | 'long-hair' | 'cap' | 'bow' | 'beard' | 'headphones'

// A shared 14 × 14 silhouette keeps faces readable even at 20 px.
export function getPortraitPixels(variant: PortraitVariant): string[][] {
  const pixels = Array.from({ length: 14 }, () => Array<string>(14).fill('.'))
  const fill = (x: number, y: number, width: number, height: number, color: string) => {
    for (let row = y; row < y + height; row++)
      for (let column = x; column < x + width; column++) pixels[row]![column] = color
  }
  fill(2, 10, 10, 4, 'o')
  fill(1, 11, 12, 2, 'o')
  fill(3, 10, 8, 4, 't')
  fill(1, 11, 2, 1, 'h')
  fill(11, 11, 2, 1, 'h')
  fill(3, 1, 8, 10, 'o')
  fill(4, 0, 6, 1, 'o')
  fill(2, 3, 10, 7, 'o')
  fill(3, 3, 8, 7, 'h')
  fill(4, 1, 6, 3, 'h')
  fill(4, 2, 5, 1, 'l')
  fill(3, 3, 1, 3, 'l')
  fill(4, 4, 6, 5, 'f')
  fill(4, 4, 1, 1, 'h')
  fill(9, 4, 1, 1, 'h')
  fill(4, 8, 1, 1, 'h')
  fill(9, 8, 1, 1, 'h')
  fill(5, 6, 1, 1, 'e')
  fill(8, 6, 1, 1, 'e')
  fill(6, 9, 2, 1, 'f')
  fill(6, 10, 2, 1, 'g')
  fill(4, 11, 1, 2, 'l')
  fill(9, 11, 1, 2, 'h')
  fill(6, 12, 2, 2, 'h')
  if (variant === 'long-hair' || variant === 'bow') {
    fill(3, 4, 1, 8, 'a')
    fill(10, 4, 1, 8, 'a')
    fill(4, 4, 2, 1, 'a')
    fill(9, 4, 1, 2, 'a')
    fill(2, 9, 1, 3, 'a')
    fill(11, 9, 1, 3, 'a')
    if (variant === 'bow') {
      fill(9, 2, 3, 1, 'g')
      fill(10, 3, 1, 1, 'g')
      fill(4, 5, 1, 1, 'a')
    }
  }
  if (variant === 'cap') {
    fill(3, 1, 8, 2, 'h')
    fill(4, 1, 5, 1, 'l')
    fill(2, 3, 11, 1, 'o')
    fill(3, 3, 9, 1, 'h')
    fill(9, 4, 4, 1, 'l')
    fill(3, 9, 1, 1, 't')
    fill(10, 9, 1, 1, 't')
  }
  if (variant === 'beard') {
    fill(4, 2, 6, 1, 'a')
    fill(4, 3, 1, 2, 'a')
    fill(5, 9, 4, 1, 'a')
    fill(9, 10, 1, 4, 'g')
  }
  if (variant === 'headphones') {
    fill(2, 5, 1, 3, 'g')
    fill(11, 5, 1, 3, 'g')
    fill(4, 9, 6, 1, 'l')
    fill(5, 10, 4, 1, 'g')
    fill(8, 11, 1, 2, 'g')
  }
  return pixels
}

// Adapter for existing shell identities; the pixel primitives themselves use generic variants.
export function getAvatarPixels(id: AgentId): string[][] {
  const variants: Partial<Record<AgentId, PortraitVariant>> = {}
  return getPortraitPixels(variants[id] ?? 'hood')
}
