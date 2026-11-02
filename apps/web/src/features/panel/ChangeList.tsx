import { changeBar } from '@/features/chat/change-bar'
import { useRef, useState } from 'react'
import { useVirtualizer } from '@tanstack/react-virtual'
import { ChevronRight, File, FileCode2, FileJson, FileText, PanelRightOpen } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { FileChange } from '@/lib/rpc/contract'
import { openWorkspaceFile } from '@/features/files/open'
import '@/styles/redesign/changes.css'

const iconFor = (path: string) =>
  /\.(tsx?|jsx?|go|py|rs|css|html)$/.test(path)
    ? FileCode2
    : /\.json$/.test(path)
      ? FileJson
      : /\.(md|txt)$/.test(path)
        ? FileText
        : File

/** Inline diff rows: old/new line numbers, sign and text, one row per hunk line. */
export function diffRows(change: FileChange) {
  const rows: {
    kind: 'add' | 'del' | 'ctx' | 'gap'
    old?: number
    new?: number
    text: string
  }[] = []
  change.hunks.forEach((hunk, index) => {
    if (index > 0 || hunk.old_start > 1 || hunk.new_start > 1)
      rows.push({ kind: 'gap', text: `@@ −${hunk.old_start} +${hunk.new_start} @@` })
    let oldLine = hunk.old_start
    let newLine = hunk.new_start
    for (const line of hunk.lines) {
      const sign = line[0]
      const text = line.slice(1)
      if (sign === '+') rows.push({ kind: 'add', new: newLine++, text })
      else if (sign === '-') rows.push({ kind: 'del', old: oldLine++, text })
      else rows.push({ kind: 'ctx', old: oldLine++, new: newLine++, text })
    }
  })
  return rows
}

export function ChangeList({
  changes,
  id,
  onOpen,
}: {
  changes: FileChange[]
  id?: string
  onOpen?: () => void
}) {
  const { t } = useTranslation()
  const total = changes.reduce((sum, c) => sum + c.added + c.removed, 0)
  const [open, setOpen] = useState<Record<string, boolean>>(() =>
    Object.fromEntries(changes.map((c, i) => [c.path, total <= 80 || i === 0])),
  )
  const openInFiles = (change: FileChange) => {
    void openWorkspaceFile(change.path, change).then(() => onOpen?.())
  }
  const parent = useRef<HTMLDivElement>(null)
  const virtualized = changes.length > 30
  // oxlint-disable-next-line react/incompatible-library -- Measured diff rows intentionally use the virtualizer's mutable instance.
  const virtual = useVirtualizer({
    count: virtualized ? changes.length : 0,
    getScrollElement: () => parent.current,
    estimateSize: () => 56,
    overscan: 4,
  })
  const visible = virtualized
    ? virtual
        .getVirtualItems()
        .map((row) => ({ change: changes[row.index]!, index: row.index, row }))
    : changes.map((change, index) => ({ change, index, row: undefined }))
  return (
    <div ref={parent} className="change-scroll">
      <ul
        style={
          virtualized
            ? {
                height: virtual.getTotalSize(),
                position: 'relative',
                maxHeight: 'none',
                overflow: 'visible',
              }
            : undefined
        }
        id={id}
        className="changes-list change-animate"
        aria-label={t('changesCount', { count: changes.length })}
      >
        {visible.map(({ change, index, row }) => {
          const Icon = iconFor(change.path)
          const slash = change.path.lastIndexOf('/') + 1
          const expanded = !change.binary && (open[change.path] ?? false)
          const rows = expanded ? diffRows(change) : []
          return (
            <li
              key={change.path}
              data-index={index}
              ref={virtualized ? virtual.measureElement : undefined}
              className={expanded ? 'change-item open' : 'change-item'}
              style={{
                animationDelay: `${Math.min(index, 7) * 35}ms`,
                ...(row
                  ? {
                      animation: 'none',
                      position: 'absolute',
                      top: 0,
                      left: 0,
                      width: '100%',
                      transform: `translateY(${row.start}px)`,
                    }
                  : {}),
              }}
            >
              <div className="change-row">
                <button
                  className="change-toggle"
                  aria-expanded={expanded}
                  aria-label={t(expanded ? 'changeHideDiff' : 'changeShowDiff', {
                    path: change.path,
                  })}
                  disabled={change.binary}
                  onClick={() => setOpen((s) => ({ ...s, [change.path]: !expanded }))}
                >
                  <ChevronRight size={14} className="change-caret" aria-hidden="true" />
                  <Icon size={16} aria-hidden="true" />
                </button>
                <button className="change-path" onClick={() => openInFiles(change)}>
                  <span>{change.path.slice(0, slash)}</span>
                  <strong>{change.path.slice(slash)}</strong>
                </button>
                <span className="change-metadata">
                  {change.status !== 'modified' && (
                    <small className={`change-chip ${change.status}`}>
                      {t(change.status === 'added' ? 'changeAdded' : 'changeDeleted')}
                    </small>
                  )}
                  <span className="change-counts">
                    <span className="change-add">+{change.added}</span>{' '}
                    <span className="change-del">−{change.removed}</span>
                  </span>
                  <span className="change-bar" aria-hidden="true">
                    {changeBar(change.added, change.removed).map((kind, i) => (
                      <i key={i} className={kind} style={{ animationDelay: `${i * 45}ms` }} />
                    ))}
                  </span>
                </span>
                <button
                  className="change-open"
                  title={t('changeOpenInFiles')}
                  aria-label={t('changeOpenFile', { path: change.path })}
                  onClick={() => openInFiles(change)}
                >
                  {change.binary ? (
                    t('panelOpenFile')
                  ) : (
                    <PanelRightOpen size={15} aria-hidden="true" />
                  )}
                </button>
              </div>
              {expanded && (
                <div className="change-diff" role="table" aria-label={change.path}>
                  {rows.map((row, i) => (
                    <div key={i} role="row" className={`diff-row diff-${row.kind}`}>
                      {row.kind === 'gap' ? (
                        <span className="diff-gap" role="cell">
                          {row.text}
                        </span>
                      ) : (
                        <>
                          <span className="diff-num" role="cell">
                            {row.old ?? ''}
                          </span>
                          <span className="diff-num" role="cell">
                            {row.new ?? ''}
                          </span>
                          <span className="diff-sign" role="cell" aria-hidden="true">
                            {row.kind === 'add' ? '+' : row.kind === 'del' ? '−' : ' '}
                          </span>
                          <code role="cell">{row.text || ' '}</code>
                        </>
                      )}
                    </div>
                  ))}
                  {change.truncated && <p className="diff-more">{t('changeTruncated')}</p>}
                </div>
              )}
            </li>
          )
        })}
      </ul>
    </div>
  )
}
