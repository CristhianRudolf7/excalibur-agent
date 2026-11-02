import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import i18n from '@/lib/i18n'
import type { FileChange } from '@/lib/rpc/contract'
import { useFiles } from '@/features/files/store'
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
it('renders totals, plural forms, status and opens the selected change', async () => {
  await i18n.changeLanguage('en')
  const onFile = vi.fn()
  const view = render(<ChangesCard changes={[change]} onFile={onFile} />)
  expect(screen.getByText('1 file changed')).toBeVisible()
  expect(screen.getByText('new', { exact: true })).toBeVisible()
  fireEvent.click(screen.getByRole('button', { name: `Open ${change.path} in Files` }))
  await waitFor(() => expect(onFile).toHaveBeenCalledOnce())
  expect(useFiles.getState().selected).toBe(change.path)
  expect(useFiles.getState().changes[change.path]).toEqual(change)
  view.rerender(
    <ChangesCard changes={[change, { ...change, path: 'other.ts', added: 1 }]} onFile={onFile} />,
  )
  expect(screen.getByText('2 files changed')).toBeVisible()
  expect(screen.getByText('+9')).toBeVisible()
  expect(screen.getByText('−4')).toBeVisible()
  await i18n.changeLanguage('es')
  expect(screen.getByText('2 archivos modificados')).toBeVisible()
  fireEvent.click(screen.getByRole('button', { name: /2 archivos modificados/ }))
  expect(screen.queryByRole('list')).not.toBeInTheDocument()
})
it('shows inline diffs like an edit list and opens only the first when the set is large', () => {
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
  const big = Array.from({ length: 5 }, (_, i) => ({ ...modified, path: `${i}.ts`, added: 40 }))
  render(<ChangesCard changes={big} onFile={() => {}} />)
  expect(screen.getAllByRole('table')).toHaveLength(1)
  const second = screen.getAllByRole('button', { expanded: false })[0]!
  fireEvent.click(second)
  expect(screen.getAllByRole('table')).toHaveLength(2)
})
it('allocates five squares proportionally with both kinds represented', () => {
  expect(changeBar(0, 0)).toEqual(Array(5).fill('empty'))
  expect(changeBar(4, 0)).toEqual(Array(5).fill('add'))
  expect(changeBar(0, 4)).toEqual(Array(5).fill('del'))
  expect(changeBar(9, 1)).toEqual(['add', 'add', 'add', 'add', 'del'])
  expect(changeBar(1, 9)).toEqual(['add', 'del', 'del', 'del', 'del'])
})
