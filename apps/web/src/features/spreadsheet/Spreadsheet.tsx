import { useMemo, useRef, useState, useEffect, useLayoutEffect } from 'react'
import type { KeyboardEvent } from 'react'
import { useVirtualizer } from '@tanstack/react-virtual'
import { useStore } from 'zustand'
import { useTranslation } from 'react-i18next'
import { CheckCircle2, ChevronDown, Ellipsis, Plus, Sheet as SheetIcon } from 'lucide-react'
import { IconButton } from '@/components/shared'
import { Button } from '@/components/ui/button'
import { cellAddress, columnName, evaluateCell, coordinates, type Cells } from './formulas'
import { useWorkbook, formatValue, type CellFormat } from './store'
import { translatedCells } from './mock-data'

type Position = [number, number]
export function Spreadsheet() {
  const { t, i18n } = useTranslation()
  const book = useWorkbook()
  const sheet = book.sheets.find((s) => s.id === book.active)!
  const sheetData = useStore(sheet.store)
  const viewport = useRef<HTMLDivElement>(null)
  const grid = useRef<HTMLDivElement>(null)
  const [active, setActive] = useState<Position>(sheet.id.startsWith('loaded-') ? [0, 0] : [3, 7])
  const [anchor, setAnchor] = useState<Position>(active)
  // A freshly loaded workspace file starts at A1 so its first columns are visible.
  const [loadedFor, setLoadedFor] = useState(sheet.id)
  if (loadedFor !== sheet.id) {
    setLoadedFor(sheet.id)
    if (sheet.id.startsWith('loaded-')) {
      setActive([0, 0])
      setAnchor([0, 0])
    }
  }
  const [editing, setEditing] = useState<string | null>(null)
  const [formulaDraft, setFormulaDraft] = useState<string | null>(null)
  const userResized = useRef(false)
  const [viewportWidth, setViewportWidth] = useState(0)
  const [widths, setWidths] = useState<number[]>(() =>
    Array.from({ length: 26 }, (_, col) => (col === 0 ? 120 : col === 1 ? 100 : 84)),
  )
  const dragging = useRef(false)
  const [menu, setMenu] = useState<string | null>(null)
  const cells = useMemo(
    () => ({
      ...(sheet.id === 'summary'
        ? Object.fromEntries(
            Object.entries(translatedCells).map(([address, key]) => [address, t(key)]),
          )
        : {}),
      ...sheetData.cells,
    }),
    [sheetData.cells, sheet.id, t],
  )
  const [rowCount, columnCount] = useMemo(() => {
    let rows = 100,
      columns = 26
    for (const address of Object.keys(cells)) {
      const [row, column] = coordinates(address)
      rows = Math.max(rows, row + 1)
      columns = Math.max(columns, column + 1)
    }
    return [rows, columns]
  }, [cells])
  const address = cellAddress(...active)
  const raw = String(cells[address] ?? '')
  // oxlint-disable-next-line react/incompatible-library -- Virtualizer measurements intentionally remain outside React Compiler memoization.
  const rows = useVirtualizer({
    count: rowCount,
    getScrollElement: () => viewport.current,
    estimateSize: () => 29,
    overscan: 5,
    scrollMargin: 29,
    scrollPaddingStart: 29,
    scrollPaddingEnd: 2,
  })
  // oxlint-disable-next-line react/incompatible-library -- Virtualizer measurements intentionally remain outside React Compiler memoization.
  const columns = useVirtualizer({
    horizontal: true,
    count: columnCount,
    getScrollElement: () => viewport.current,
    estimateSize: (index) => widths[index] ?? 84,
    overscan: 2,
    scrollMargin: 38,
    scrollPaddingStart: 38,
    scrollPaddingEnd: 2,
  })
  useLayoutEffect(() => {
    const node = viewport.current
    if (!node) return
    const fit = () => {
      const available = node.clientWidth
      setViewportWidth(available)
      if (userResized.current) return
      // The requested 120/100/84 proportions total 762 px with the row header.
      // Scale those defaults to fit desktop panels while keeping narrow sheets readable.
      const scale = Math.min(1, (Math.max(600, available - 8) - 38) / 724)
      setWidths(
        Array.from(
          { length: 26 },
          (_, column) => (column === 0 ? 120 : column === 1 ? 100 : 84) * scale,
        ),
      )
    }
    fit()
    const observer = new ResizeObserver(fit)
    observer.observe(node)
    return () => observer.disconnect()
  }, [])
  useEffect(() => {
    columns.measure()
  }, [widths, columns])
  useEffect(() => {
    const stop = () => {
      dragging.current = false
    }
    window.addEventListener('pointerup', stop)
    return () => window.removeEventListener('pointerup', stop)
  }, [])
  useLayoutEffect(() => {
    const frame = requestAnimationFrame(() => {
      rows.scrollToIndex(active[0], { align: 'auto' })
      const budgetFits = widths.slice(0, 8).reduce((sum, width) => sum + width, 38) <= viewportWidth
      if (active[1] < 8 && budgetFits && viewport.current) viewport.current.scrollLeft = 0
      else columns.scrollToIndex(active[1], { align: 'auto' })
    })
    return () => cancelAnimationFrame(frame)
  }, [active, widths, viewportWidth, sheet.id, rows, columns])
  const selectedAddresses = () => {
    const result: string[] = []
    for (let r = Math.min(active[0], anchor[0]); r <= Math.max(active[0], anchor[0]); r++)
      for (let c = Math.min(active[1], anchor[1]); c <= Math.max(active[1], anchor[1]); c++)
        result.push(cellAddress(r, c))
    return result
  }
  const move = (r: number, c: number, extend = false) => {
    const next: Position = [
      Math.max(0, Math.min(rowCount - 1, r)),
      Math.max(0, Math.min(columnCount - 1, c)),
    ]
    setActive(next)
    if (!extend) setAnchor(next)
    setFormulaDraft(null)
  }
  const commit = (value: string) => {
    book.setCells({
      [address]:
        value !== '' && !value.startsWith('=') && Number.isFinite(Number(value))
          ? Number(value)
          : value,
    })
    setEditing(null)
    setFormulaDraft(null)
  }
  const keyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.target !== grid.current) return
    const command = event.metaKey || event.ctrlKey
    if (command && event.key.toLowerCase() === 'z') {
      event.preventDefault()
      if (event.shiftKey) book.redo()
      else book.undo()
      return
    }
    if (command) return
    const moves: Record<string, Position> = {
      ArrowUp: [-1, 0],
      ArrowDown: [1, 0],
      ArrowLeft: [0, -1],
      ArrowRight: [0, 1],
      Tab: [0, event.shiftKey ? -1 : 1],
    }
    const delta = moves[event.key]
    if (delta) {
      event.preventDefault()
      move(active[0] + delta[0], active[1] + delta[1], event.shiftKey && event.key !== 'Tab')
    } else if (event.key === 'Enter' || event.key === 'F2') {
      event.preventDefault()
      setEditing(raw)
    } else if (event.key === 'Delete' || event.key === 'Backspace') {
      event.preventDefault()
      book.setCells(Object.fromEntries(selectedAddresses().map((a) => [a, ''])))
    } else if (event.key.length === 1) {
      event.preventDefault()
      setEditing(event.key)
    }
  }
  const format = (kind?: CellFormat['kind'], delta = 0) => {
    const current = sheetData.formats[address]
    book.setFormats(selectedAddresses(), {
      kind: kind ?? current?.kind ?? 'number',
      decimals: Math.max(0, Math.min(8, (current?.decimals ?? 2) + delta)),
    })
  }
  const sum = selectedAddresses().reduce((sum, a) => {
    const value = evaluateCell(cells, a)
    return sum + (typeof value === 'number' ? value : 0)
  }, 0)
  return (
    <section className="spreadsheet" aria-label={t('grid')}>
      <div className="sheet-title">
        <SheetIcon className="sheet-symbol" />
        <h2>{sheet.id.startsWith('loaded-') ? (book.source ?? sheet.title) : t('budget')}</h2>
        <IconButton label="more" onClick={() => setMenu(menu ? null : 'more')}>
          <Ellipsis />
        </IconButton>
      </div>
      <div className="sheet-toolbar">
        {['fileMenu', 'edit', 'view'].map((key) => (
          <Button key={key} onClick={() => setMenu(menu === key ? null : key)}>
            {t(key)}
            <ChevronDown />
          </Button>
        ))}
        <span className="toolbar-divider" />
        <IconButton label="currency" onClick={() => format('currency')}>
          €
        </IconButton>
        <IconButton label="percent" onClick={() => format('percent')}>
          %
        </IconButton>
        <IconButton label="fewerDecimals" onClick={() => format(undefined, -1)}>
          .0↓
        </IconButton>
        <IconButton label="moreDecimals" onClick={() => format(undefined, 1)}>
          .00↑
        </IconButton>
      </div>
      {menu && (
        <div className="sheet-menu" role="status">
          {t('soon')}
          <IconButton label="close" onClick={() => setMenu(null)}>
            ×
          </IconButton>
        </div>
      )}
      <div className="formula-bar">
        <input
          aria-label={t('activeCell')}
          value={address}
          onChange={(event) => {
            const [r, c] = coordinates(event.target.value.toUpperCase())
            if (r >= 0 && r < rowCount && c >= 0 && c < columnCount) move(r, c)
          }}
        />
        <span>ƒx</span>
        <input
          aria-label={t('formula')}
          value={formulaDraft ?? raw}
          onChange={(event) => setFormulaDraft(event.target.value)}
          onBlur={() => {
            if (formulaDraft !== null) commit(formulaDraft)
          }}
          onKeyDown={(event) => {
            if (event.key === 'Enter') {
              commit(formulaDraft ?? raw)
              requestAnimationFrame(() => grid.current?.focus())
            }
            if (event.key === 'Escape') {
              event.preventDefault()
              event.stopPropagation()
              setFormulaDraft(null)
              requestAnimationFrame(() => grid.current?.focus())
            }
          }}
        />
      </div>
      <div className="sheet-viewport" ref={viewport}>
        <div
          ref={grid}
          role="grid"
          aria-label={t('grid')}
          aria-rowcount={101}
          aria-colcount={27}
          aria-activedescendant={`cell-${address}`}
          tabIndex={0}
          className="sheet-grid"
          style={{ width: columns.getTotalSize() + 38, height: rows.getTotalSize() + 29 }}
          onKeyDown={keyDown}
          onCopy={(event) => {
            if (event.target instanceof HTMLInputElement) return
            const lines: string[] = []
            for (let r = Math.min(active[0], anchor[0]); r <= Math.max(active[0], anchor[0]); r++) {
              const values: string[] = []
              for (let c = Math.min(active[1], anchor[1]); c <= Math.max(active[1], anchor[1]); c++)
                values.push(String(evaluateCell(cells, cellAddress(r, c))))
              lines.push(values.join('\t'))
            }
            event.clipboardData.setData('text/plain', lines.join('\n'))
            event.preventDefault()
          }}
          onPaste={(event) => {
            if (event.target instanceof HTMLInputElement) return
            event.preventDefault()
            const updates: Cells = {}
            event.clipboardData
              .getData('text/plain')
              .replace(/\r/g, '')
              .replace(/\n$/, '')
              .split('\n')
              .forEach((line, r) =>
                line.split('\t').forEach((value, c) => {
                  if (active[0] + r < rowCount && active[1] + c < columnCount)
                    updates[cellAddress(active[0] + r, active[1] + c)] =
                      value !== '' && Number.isFinite(Number(value)) ? Number(value) : value
                }),
              )
            book.setCells(updates)
          }}
        >
          <div className="column-headers" role="row" style={{ width: columns.getTotalSize() + 38 }}>
            <div className="sheet-corner" />
            {columns.getVirtualItems().map((col) => (
              <div
                key={col.key}
                role="columnheader"
                className={active[1] === col.index ? 'active-header' : ''}
                style={{ position: 'absolute', left: col.start, width: col.size, height: 29 }}
              >
                {columnName(col.index)}
                <span
                  className="column-resizer"
                  onPointerDown={(event) => {
                    event.preventDefault()
                    userResized.current = true
                    const start = event.clientX,
                      width = widths[col.index] ?? 84
                    const drag = (e: PointerEvent) =>
                      setWidths((w) =>
                        Array.from({ length: Math.max(w.length, col.index + 1) }, (_, i) =>
                          i === col.index ? Math.max(64, width + e.clientX - start) : (w[i] ?? 84),
                        ),
                      )
                    const stop = () => {
                      window.removeEventListener('pointermove', drag)
                      window.removeEventListener('pointerup', stop)
                    }
                    window.addEventListener('pointermove', drag)
                    window.addEventListener('pointerup', stop)
                  }}
                />
              </div>
            ))}
          </div>
          {rows.getVirtualItems().map((row) => (
            <div
              role="row"
              aria-rowindex={row.index + 2}
              key={row.key}
              style={{
                position: 'absolute',
                top: row.start,
                height: row.size,
                width: columns.getTotalSize() + 38,
              }}
            >
              <div
                role="rowheader"
                className={`row-header ${active[0] === row.index ? 'active-header' : ''}`}
              >
                {row.index + 1}
              </div>
              {columns.getVirtualItems().map((col) => {
                const a = cellAddress(row.index, col.index)
                const value = evaluateCell(cells, a)
                const isActive = a === address
                const inRange =
                  row.index >= Math.min(active[0], anchor[0]) &&
                  row.index <= Math.max(active[0], anchor[0]) &&
                  col.index >= Math.min(active[1], anchor[1]) &&
                  col.index <= Math.max(active[1], anchor[1])
                return (
                  <div
                    role="gridcell"
                    id={`cell-${a}`}
                    aria-label={t('sheetCell', { cell: a })}
                    aria-selected={inRange}
                    key={col.key}
                    className={`sheet-cell ${inRange ? 'in-range' : ''} ${isActive ? 'active-cell' : ''} ${row.index === 0 || row.index === 7 ? 'bold-cell' : ''} ${typeof value === 'number' ? 'numeric-cell' : ''}`}
                    style={{ left: col.start, width: col.size }}
                    onPointerDown={(event) => {
                      if (editing !== null) return
                      event.preventDefault()
                      window.getSelection()?.removeAllRanges()
                      grid.current?.focus()
                      setActive([row.index, col.index])
                      if (!event.shiftKey) setAnchor([row.index, col.index])
                      dragging.current = true
                      setFormulaDraft(null)
                    }}
                    onPointerEnter={() => {
                      if (dragging.current) setActive([row.index, col.index])
                    }}
                    onDoubleClick={() => setEditing(String(cells[a] ?? ''))}
                  >
                    {isActive && editing !== null ? (
                      <input
                        autoFocus
                        aria-label={t('sheetCell', { cell: a })}
                        value={editing}
                        onChange={(event) => setEditing(event.target.value)}
                        onBlur={() => commit(editing)}
                        onKeyDown={(event) => {
                          event.stopPropagation()
                          if (event.key === 'Escape') {
                            event.preventDefault()
                            setEditing(null)
                            requestAnimationFrame(() => grid.current?.focus())
                          }
                          if (event.key === 'Enter' || event.key === 'Tab') {
                            event.preventDefault()
                            commit(editing)
                            if (event.key === 'Enter')
                              move(active[0] + (event.shiftKey ? -1 : 1), active[1])
                            else move(active[0], active[1] + (event.shiftKey ? -1 : 1))
                            requestAnimationFrame(() => grid.current?.focus())
                          }
                        }}
                      />
                    ) : (
                      formatValue(value, sheetData.formats[a], i18n.language)
                    )}
                    {isActive && <span className="fill-handle" />}
                  </div>
                )
              })}
            </div>
          ))}
        </div>
      </div>
      <div className="sheet-bottom">
        <IconButton label="addSheet" onClick={book.addSheet}>
          <Plus />
        </IconButton>
        <div className="sheet-tabs" role="tablist" aria-label={t('sheets')}>
          {book.sheets.map((s, index) => (
            <button
              key={s.id}
              role="tab"
              aria-selected={s.id === sheet.id}
              onClick={() => {
                book.selectSheet(s.id)
                setEditing(null)
                setFormulaDraft(null)
              }}
            >
              {s.id.startsWith('loaded-')
                ? s.title
                : s.title.startsWith('sheet-')
                  ? t('newSheet', { number: index + 1 })
                  : t(s.title)}
            </button>
          ))}
        </div>
        <span className="saved">
          <CheckCircle2 />
          {t('saved')}
        </span>
        <span>
          {t('sum', { value: formatValue(sum, { kind: 'currency', decimals: 2 }, i18n.language) })}
        </span>
      </div>
    </section>
  )
}
