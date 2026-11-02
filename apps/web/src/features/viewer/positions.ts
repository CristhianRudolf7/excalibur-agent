import type { Block, Box, ChunkDetail } from './types'

export function isHighlighted(block: Block, index: number, page: number, chunk?: ChunkDetail) {
  if (!chunk || chunk.page !== page) return false
  return chunk.anchor ? block.anchor === chunk.anchor : chunk.paragraph === index + 1
}

export function blobURL(digest: string) {
  return /^[a-f0-9]{64}$/i.test(digest) ? `/blobs/${digest}` : undefined
}

export function scaleBox(box: Box, width: number, pageWidth: number) {
  const scale = pageWidth > 0 && Number.isFinite(pageWidth) ? width / pageWidth : 0
  return {
    left: box.x0 * scale,
    top: box.y0 * scale,
    width: Math.max(0, box.x1 - box.x0) * scale,
    height: Math.max(0, box.y1 - box.y0) * scale,
  }
}

export function cellPosition(anchor: string) {
  const match = /!\$?([A-Z]{1,3})\$?([1-9]\d*)$/i.exec(anchor)
  if (!match) return undefined
  const column = [...match[1]!.toUpperCase()].reduce((n, c) => n * 26 + c.charCodeAt(0) - 64, 0)
  const row = Number(match[2])
  if (column > 16384 || row > 1048576) return undefined
  return { row: row - 1, column: column - 1 }
}

export function columnName(column: number): string {
  let name = ''
  for (let n = column + 1; n > 0; n = Math.floor((n - 1) / 26))
    name = String.fromCharCode(65 + ((n - 1) % 26)) + name
  return name
}
