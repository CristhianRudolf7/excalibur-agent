import type { editor } from 'monaco-editor'
import type { FileChange } from '@/lib/rpc/contract'

export function removedContent(change: FileChange) {
  return change.hunks
    .flatMap((hunk) =>
      hunk.lines.filter((line) => line.startsWith('-')).map((line) => line.slice(1)),
    )
    .join('\n')
}
export function changeDecorations(
  change: FileChange,
  lineCount: number,
): editor.IModelDeltaDecoration[] {
  const result: editor.IModelDeltaDecoration[] = []
  let deletedLine = 1
  for (const hunk of change.hunks) {
    let line = Math.max(1, hunk.new_start)
    for (let i = 0; i < hunk.lines.length; i++) {
      const text = hunk.lines[i]!
      const removed = text.startsWith('-')
      if (removed && change.status !== 'deleted') {
        while (hunk.lines[i + 1]?.startsWith('-')) i++
        continue
      }
      if (text.startsWith('+') || removed) {
        const removedLines = [text.slice(1)]
        if (removed)
          while (hunk.lines[i + 1]?.startsWith('-')) removedLines.push(hunk.lines[++i]!.slice(1))
        const start = Math.min(lineCount, change.status === 'deleted' ? deletedLine : line)
        const hover = removed
          ? { value: removedLines.join('\n\n'), isTrusted: false, supportHtml: false }
          : undefined
        result.push({
          range: {
            startLineNumber: start,
            startColumn: 1,
            endLineNumber:
              change.status === 'deleted'
                ? Math.min(lineCount, start + removedLines.length - 1)
                : start,
            endColumn: 1,
          },
          options: {
            isWholeLine: true,
            className: removed
              ? change.status === 'deleted'
                ? 'diff-removed diff-deleted-line'
                : 'diff-removed'
              : 'diff-added',
            glyphMarginClassName: removed ? 'diff-minus' : undefined,
            linesDecorationsClassName: removed ? 'diff-minus' : 'diff-plus',
            glyphMarginHoverMessage: hover,
            hoverMessage: hover,
          },
        })
        if (removed) deletedLine += removedLines.length
      }
      if (!removed) line++
    }
  }
  return result
}
export function hunkPositions(change: FileChange) {
  if (change.status !== 'deleted') return change.hunks.map((hunk) => Math.max(1, hunk.new_start))
  let line = 1
  return change.hunks.map((hunk) => {
    const start = line
    line += hunk.lines.filter((text) => text.startsWith('-')).length
    return start
  })
}

/** Runs of removed lines placed before a line of the new file (inline diff). */
export interface RemovedZone {
  afterLine: number
  lines: string[]
}
export function removedZones(change: FileChange): RemovedZone[] {
  if (change.status === 'deleted') return []
  const zones: RemovedZone[] = []
  for (const hunk of change.hunks) {
    let line = Math.max(1, hunk.new_start)
    for (let i = 0; i < hunk.lines.length; i++) {
      const text = hunk.lines[i]!
      if (text.startsWith('-')) {
        const lines = [text.slice(1)]
        while (hunk.lines[i + 1]?.startsWith('-')) lines.push(hunk.lines[++i]!.slice(1))
        zones.push({ afterLine: line - 1, lines })
        continue
      }
      line++
    }
  }
  return zones
}
