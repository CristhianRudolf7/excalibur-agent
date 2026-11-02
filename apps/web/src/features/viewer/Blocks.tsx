import { isHighlighted, blobURL } from './positions'
import { useEffect, useRef, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { Markdown } from '@/components/markdown/Markdown'
import type { Block, ChunkDetail, Page } from './types'

function BlockContent({ block }: { block: Block }) {
  let text: ReactNode = block.text
  if (block.style?.includes('code')) text = <code>{text}</code>
  if (block.style?.includes('italic')) text = <em>{text}</em>
  if (block.style?.includes('bold')) text = <strong>{text}</strong>
  if (block.kind === 'heading') {
    switch (block.level) {
      case 1:
        return <h1>{text}</h1>
      case 2:
        return <h2>{text}</h2>
      case 3:
        return <h3>{text}</h3>
      case 4:
        return <h4>{text}</h4>
      case 5:
        return <h5>{text}</h5>
      default:
        return <h6>{text}</h6>
    }
  }
  if (block.kind === 'table') return <Markdown>{block.text}</Markdown>
  if (block.kind === 'image')
    return blobURL(block.image ?? '') ? (
      <figure>
        <img loading="lazy" src={blobURL(block.image!)} alt={block.text} />
        <figcaption>{block.text}</figcaption>
      </figure>
    ) : (
      <p>{text}</p>
    )
  if (block.kind === 'list_item' || (block.kind === 'slide_text' && block.level))
    return (
      <ul style={{ paddingInlineStart: `${Math.min(8, Math.max(1, block.level ?? 1)) * 1.25}rem` }}>
        <li>{text}</li>
      </ul>
    )
  return <p>{text}</p>
}
export function Blocks({
  page,
  chunk,
  notes = true,
  focusKey,
}: {
  page: Page
  chunk?: ChunkDetail
  notes?: boolean
  focusKey?: unknown
}) {
  const { t } = useTranslation('viewer')
  const root = useRef<HTMLDivElement>(null)
  useEffect(() => {
    root.current?.querySelector('[data-highlight="true"]')?.scrollIntoView?.({ block: 'center' })
  }, [page, chunk, notes, focusKey])
  const render = (block: Block, index: number) => (
    <div
      key={index}
      data-anchor={block.anchor}
      data-highlight={isHighlighted(block, index, page.number, chunk)}
      className={isHighlighted(block, index, page.number, chunk) ? 'viewer-highlight' : undefined}
    >
      <BlockContent block={block} />
    </div>
  )
  return (
    <div ref={root} className="viewer-blocks">
      {(page.blocks ?? []).map((block, i) => block.kind !== 'note' && render(block, i))}
      {notes && (page.blocks ?? []).some((b) => b.kind === 'note') && (
        <aside className="viewer-notes">
          <h3>{t('notes')}</h3>
          {(page.blocks ?? []).map((block, i) => block.kind === 'note' && render(block, i))}
        </aside>
      )}
    </div>
  )
}
