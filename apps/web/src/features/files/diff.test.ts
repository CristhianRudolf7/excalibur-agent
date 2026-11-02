import { expect, it } from 'vitest'
import type { FileChange } from '@/lib/rpc/contract'
import { changeDecorations, hunkPositions, removedContent, removedZones } from './diff'
const change: FileChange = {
  path: 'file.ts',
  status: 'modified',
  added: 1,
  removed: 2,
  hunks: [
    {
      old_start: 2,
      old_lines: 3,
      new_start: 2,
      new_lines: 2,
      lines: [' context', '-old', '-older', '+new'],
    },
  ],
}
it('decorates additions and places grouped removals as ghost rows above them', () => {
  const decorations = changeDecorations(change, 8)
  expect(decorations.map((d) => [d.range.startLineNumber, d.options.className])).toEqual([
    [3, 'diff-added'],
  ])
  // Removed lines sit between line 2 (context) and line 3 (the addition).
  expect(removedZones(change)).toEqual([{ afterLine: 2, lines: ['old', 'older'] }])
  expect(changeDecorations(change, 1)[0]?.range.startLineNumber).toBe(1)
  expect(hunkPositions(change)).toEqual([2])
})
it('reconstructs deleted content and uses its display positions', () => {
  const deleted = { ...change, status: 'deleted' as const }
  expect(removedContent(deleted)).toBe('old\nolder')
  expect(changeDecorations(deleted, 2)[0]?.range).toMatchObject({
    startLineNumber: 1,
    endLineNumber: 2,
  })
  expect(hunkPositions(deleted)).toEqual([1])
})
