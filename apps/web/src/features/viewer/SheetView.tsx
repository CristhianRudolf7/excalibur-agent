import { cellPosition, columnName } from './positions'
import { useEffect, useMemo, useRef } from 'react'
import { useVirtualizer } from '@tanstack/react-virtual'
import type { ChunkDetail, Page } from './types'

export function SheetView({
  page,
  chunk,
  focusKey,
}: {
  page: Page
  chunk?: ChunkDetail
  focusKey?: unknown
}) {
  const container = useRef<HTMLDivElement>(null)
  const { cells, rows, columns } = useMemo(() => {
    const cells = new Map<string, string>()
    let rows = 1,
      columns = 1
    for (const block of page.blocks ?? []) {
      const position = cellPosition(block.anchor ?? '')
      if (!position || block.kind !== 'cell') continue
      cells.set(`${position.row}:${position.column}`, block.text)
      rows = Math.max(rows, position.row + 1)
      columns = Math.max(columns, position.column + 1)
    }
    return { cells, rows, columns }
  }, [page])
  // Virtualizer instances are intentionally mutable; this component is not compiler memoized.
  // oxlint-disable-next-line react/incompatible-library
  const rowVirtualizer = useVirtualizer({
    count: rows,
    getScrollElement: () => container.current,
    estimateSize: () => 32,
    overscan: 5,
    initialRect: { width: 600, height: 400 },
  })
  // oxlint-disable-next-line react/incompatible-library
  const columnVirtualizer = useVirtualizer({
    horizontal: true,
    count: columns,
    getScrollElement: () => container.current,
    estimateSize: () => 144,
    overscan: 2,
    initialRect: { width: 600, height: 400 },
  })
  const target = useMemo(
    () => (chunk?.page === page.number ? cellPosition(chunk.anchor) : undefined),
    [chunk, page.number],
  )
  useEffect(() => {
    if (target && target.row < rows && target.column < columns) {
      rowVirtualizer.scrollToIndex(target.row, { align: 'center' })
      columnVirtualizer.scrollToIndex(target.column, { align: 'center' })
    }
  }, [target, rows, columns, rowVirtualizer, columnVirtualizer, focusKey])
  return (
    <div ref={container} className="viewer-grid-scroll" tabIndex={0}>
      <div
        role="grid"
        aria-readonly="true"
        aria-label={page.title}
        aria-rowcount={rows}
        aria-colcount={columns}
        style={{
          position: 'relative',
          height: rowVirtualizer.getTotalSize() + 32,
          width: columnVirtualizer.getTotalSize() + 48,
        }}
      >
        <div role="row" className="viewer-grid-header" style={{ height: 32 }}>
          {columnVirtualizer.getVirtualItems().map((column) => (
            <div
              role="columnheader"
              key={column.index}
              style={{ position: 'absolute', left: column.start + 48, width: 144 }}
            >
              {columnName(column.index)}
            </div>
          ))}
        </div>
        {rowVirtualizer.getVirtualItems().map((row) => (
          <div
            role="row"
            aria-rowindex={row.index + 1}
            key={row.index}
            style={{ position: 'absolute', top: row.start + 32, height: 32, width: '100%' }}
          >
            <div role="rowheader" className="viewer-row-header">
              {row.index + 1}
            </div>
            {columnVirtualizer.getVirtualItems().map((column) => (
              <div
                role="gridcell"
                aria-colindex={column.index + 1}
                aria-label={`${columnName(column.index)}${row.index + 1}`}
                aria-selected={target?.row === row.index && target?.column === column.index}
                key={column.index}
                className="viewer-cell"
                style={{ left: column.start + 48, width: 144, height: 32 }}
              >
                {cells.get(`${row.index}:${column.index}`)}
              </div>
            ))}
          </div>
        ))}
      </div>
    </div>
  )
}
