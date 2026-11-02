import i18n from '@/lib/i18n'
import '../viewer/i18n'
import { rpc } from '@/lib/rpc/client'
import type { DocPage } from '@/lib/rpc/contract'
import { cellPosition, columnName } from '../viewer/positions'
import type { LoadedSheet } from './store'

/** Library sheets are pages with Sheet!A1 anchors and optional formula styles. */
export function mapSheets(pages: DocPage[]): LoadedSheet[] {
  return pages.map((page) => {
    const cells: LoadedSheet['cells'] = {}
    for (const block of page.blocks ?? []) {
      const position = cellPosition(block.anchor ?? '')
      if (block.kind !== 'cell' || !position) continue
      const formula = block.style?.find((style) => style.startsWith('formula:'))?.slice(8)
      const text = block.text ?? ''
      const numeric = text.trim() !== '' && /^[-+]?(?:\d+\.?\d*|\.\d+)(?:e[-+]?\d+)?$/i.test(text)
      cells[`${columnName(position.column)}${position.row + 1}`] = formula
        ? formula.startsWith('=')
          ? formula
          : `=${formula}`
        : numeric
          ? Number(text)
          : text
    }
    const anchor = page.blocks?.find((block) => block.kind === 'cell')?.anchor
    return {
      id: `loaded-${page.number}`,
      title:
        page.title ||
        anchor?.slice(0, anchor.lastIndexOf('!')) ||
        page.label ||
        `${i18n.t('viewer:sheet')} ${page.number}`,
      cells,
    }
  })
}

export async function loadWorkbook(id: string): Promise<LoadedSheet[]> {
  const { document } = await rpc.call('library.get', { id })
  if (!['ready', 'partial'].includes(document.status)) throw new Error('Workbook is not ready')
  const pages: DocPage[] = []
  for (let from = 1; from <= document.page_count; from += 20) {
    const result = await rpc.call('library.pages', {
      id,
      version: document.version,
      from,
      to: Math.min(document.page_count, from + 19),
    })
    pages.push(...result.pages)
  }
  if (!pages.length) throw new Error('Workbook has no sheets')
  return mapSheets(pages)
}
