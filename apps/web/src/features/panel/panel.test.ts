import { describe, expect, it } from 'vitest'
import { conversationChanges } from './changes'
import { mergeActivity } from './Panel'
import { validatePanel } from '@/features/shell/panels'
import { useShell } from '@/features/shell/store'
import type { ActivityItem, FileChange } from '@/lib/rpc/contract'
const change: FileChange = {
  path: 'a.ts',
  status: 'modified',
  added: 1,
  removed: 1,
  content: 'b\n',
  hunks: [{ old_start: 1, old_lines: 1, new_start: 1, new_lines: 1, lines: ['-a', '+b'] }],
}
const message = (id: string, changes: FileChange[]) => ({
  id,
  sender: 'agent' as const,
  text: '',
  time: '',
  changes,
})
describe('panel contracts', () => {
  it('starts closed and maps legacy values without exposing old tabs', () => {
    expect(useShell.getState().panelOpen).toBe(false)
    for (const value of ['library', 'invalid', undefined])
      expect(validatePanel({ panel: value }).panel).toBe('home')
    expect(validatePanel({ panel: 'subagents' }).panel).toBe('activity')
    for (const value of ['home', 'activity', 'changes', 'files', 'sheet', 'document', 'whiteboard'])
      expect(validatePanel({ panel: value }).panel).toBe(value)
  })
  it('combines earliest base with latest content and excludes user changes', () => {
    const latest = {
      ...change,
      content: 'c\n',
      hunks: [{ ...change.hunks[0]!, lines: ['-b', '+c'] }],
    }
    const changes = conversationChanges([
      message('1', [change]),
      message('2', [latest]),
      { ...message('3', [{ ...change, path: 'user.ts' }]), sender: 'user' },
    ])
    expect(changes).toHaveLength(1)
    expect(changes[0]?.hunks[0]?.lines).toEqual(['-a', '+c'])
    expect(changes[0]?.content).toBe('c\n')
  })
  it('preserves unchanged middle lines and accurate totals across separate edits', () => {
    const first = {
      ...change,
      content: 'A\nb\nc\nd\n',
      hunks: [{ old_start: 1, old_lines: 1, new_start: 1, new_lines: 1, lines: ['-a', '+A'] }],
    }
    const latest = {
      ...change,
      content: 'A\nb\nc\nD\n',
      hunks: [{ old_start: 4, old_lines: 1, new_start: 4, new_lines: 1, lines: ['-d', '+D'] }],
    }
    const result = conversationChanges([message('1', [first]), message('2', [latest])])[0]!
    expect(result.added).toBe(2)
    expect(result.removed).toBe(2)
    expect(result.hunks[0]?.lines).toEqual(['-a', '+A', ' b', ' c', '-d', '+D'])
  })
  it('keeps the latest deletion status for a file added earlier', () => {
    const first = { ...change, status: 'added' as const }
    const latest = { ...change, status: 'deleted' as const, content: '', hunks: [] }
    expect(conversationChanges([message('1', [first]), message('2', [latest])])[0]?.status).toBe(
      'deleted',
    )
  })
  it('uses the latest patch when the base cannot be reconstructed', () => {
    const latest = { ...change, content: undefined }
    expect(
      conversationChanges([message('1', [{ ...change, truncated: true }]), message('2', [latest])]),
    ).toEqual([latest])
  })
  it('keeps binary changes without trying to create text hunks', () => {
    const binary = { ...change, binary: true, content: undefined, hunks: [], added: 0, removed: 0 }
    expect(conversationChanges([message('1', [binary])])).toEqual([binary])
  })
  it('deduplicates live updates and rejects stale snapshots', () => {
    const item: ActivityItem = {
      id: 'task',
      name: 'Task',
      kind: 'task',
      state: 'running',
      started_at: '2026-11-01T10:00:00Z',
      updated_at: '2026-11-01T10:00:01Z',
    }
    const completed = { ...item, state: 'completed' as const, updated_at: '2026-11-01T10:00:02Z' }
    expect(mergeActivity([completed], item)).toEqual([completed])
    expect(mergeActivity([item], completed)).toEqual([completed])
  })
})
