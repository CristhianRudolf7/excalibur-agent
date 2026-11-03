import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import i18n from '@/lib/i18n'
import type { FileChange } from '@/lib/rpc/contract'
import { registerPanelOpener } from '@/features/files/open'
import { ChangesCard, diffRows } from './ChangesCard'
import { changeBar } from './change-bar'
const change: FileChange = {
  path: 'src/new.ts',
  status: 'added',
  added: 8,
  removed: 2,
  content: 'new',
  hunks: [],
}
afterEach(cleanup)
it('shows a one-line summary that opens the Changes panel', async () => {
  await i18n.changeLanguage('en')
  const opened: string[] = []
  registerPanelOpener((view) => opened.push(view))
  const view = render(<ChangesCard changes={[change, { ...change, path: 'other.ts', added: 1 }]} />)
  expect(screen.getByText('2 files changed')).toBeVisible()
  expect(screen.getByText('+9')).toBeVisible()
  expect(screen.getByText('−4')).toBeVisible()
  // No inline dropdowns or diffs in the chat.
  expect(screen.queryByRole('list')).not.toBeInTheDocument()
  expect(screen.queryByRole('table')).not.toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'View 2 changed files in the panel' }))
  expect(opened).toEqual(['changes'])
  await i18n.changeLanguage('es')
  view.rerender(<ChangesCard changes={[change]} />)
  expect(screen.getByText('1 archivo modificado')).toBeVisible()
  registerPanelOpener(null)
})
it('maps hunks to diff rows for the panel list', () => {
  const modified: FileChange = {
    path: 'src/app.ts',
    status: 'modified',
    added: 1,
    removed: 1,
    hunks: [
      { old_start: 3, old_lines: 2, new_start: 3, new_lines: 2, lines: [' keep', '-old', '+new'] },
    ],
  }
  expect(diffRows(modified)).toEqual([
    { kind: 'gap', text: '@@ −3 +3 @@' },
    { kind: 'ctx', old: 3, new: 3, text: 'keep' },
    { kind: 'del', old: 4, text: 'old' },
    { kind: 'add', new: 4, text: 'new' },
  ])
})
it('allocates five squares proportionally with both kinds represented', () => {
  expect(changeBar(0, 0)).toEqual(Array(5).fill('empty'))
  expect(changeBar(4, 0)).toEqual(Array(5).fill('add'))
  expect(changeBar(0, 4)).toEqual(Array(5).fill('del'))
  expect(changeBar(9, 1)).toEqual(['add', 'add', 'add', 'add', 'del'])
  expect(changeBar(1, 9)).toEqual(['add', 'del', 'del', 'del', 'del'])
})
