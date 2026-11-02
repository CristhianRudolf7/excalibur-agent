import { useEffect, useId, useRef } from 'react'
import { useVirtualizer } from '@tanstack/react-virtual'
import { useTranslation } from 'react-i18next'
import type { TurnArtifact } from './types'

/** Keep one preview mounted while the conversation's metadata list remains navigable. */
export function ArtifactHistory({
  items,
  selected,
  onSelect,
}: {
  items: TurnArtifact[]
  selected?: string
  onSelect: (id: string) => void
}) {
  const { t } = useTranslation()
  const viewport = useRef<HTMLDivElement>(null)
  const id = useId()
  const virtualized = items.length > 20
  // TanStack Virtual owns mutable scroll state; its returned methods must not be memoized.
  // eslint-disable-next-line react/incompatible-library
  const virtual = useVirtualizer({
    count: items.length,
    getScrollElement: () => viewport.current,
    estimateSize: () => 48,
    overscan: 4,
    enabled: virtualized,
    initialRect: { width: 400, height: 240 },
  })
  const rows = virtualized
    ? virtual.getVirtualItems().map((row) => ({ index: row.index, start: row.start }))
    : items.map((_, index) => ({ index, start: index * 48 }))
  const index = items.findIndex((item) => item.id === selected)
  useEffect(() => {
    if (virtualized && index >= 0) virtual.scrollToIndex(index, { align: 'auto' })
  }, [index, virtualized, virtual])
  return (
    <div
      className="artifact-history"
      ref={viewport}
      role="listbox"
      aria-label={t('artifactHistory')}
      aria-activedescendant={index >= 0 ? `${id}-${index}` : undefined}
      tabIndex={0}
      style={{ height: Math.min(240, items.length * 48) }}
      onKeyDown={(event) => {
        const next =
          event.key === 'ArrowDown'
            ? Math.min(items.length - 1, index + 1)
            : event.key === 'ArrowUp'
              ? Math.max(0, index - 1)
              : event.key === 'Home'
                ? 0
                : event.key === 'End'
                  ? items.length - 1
                  : undefined
        if (next === undefined || !items[next]) return
        event.preventDefault()
        onSelect(items[next].id)
        if (virtualized) virtual.scrollToIndex(next)
        else document.getElementById(`${id}-${next}`)?.scrollIntoView?.({ block: 'nearest' })
      }}
    >
      <div style={{ height: items.length * 48, position: 'relative' }}>
        {rows.map(({ index, start }) => {
          const item = items[index]!
          return (
            <button
              id={`${id}-${index}`}
              className="artifact-history-row"
              role="option"
              aria-selected={item.id === selected}
              aria-posinset={index + 1}
              aria-setsize={items.length}
              tabIndex={-1}
              key={item.id}
              style={{ transform: `translateY(${start}px)` }}
              onClick={() => {
                onSelect(item.id)
                viewport.current?.focus()
              }}
            >
              <span>{item.title}</span>
              <small>{item.format.toUpperCase()}</small>
            </button>
          )
        })}
      </div>
    </div>
  )
}
