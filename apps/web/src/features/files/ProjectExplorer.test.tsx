import { treeEntries, workspacePath } from './tree'
import { afterEach, expect, it, vi } from 'vitest'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { rpc } from '@/lib/rpc/client'
import type { Project } from '@/lib/rpc/contract'
import { ProjectExplorer, WorkspaceExplorer } from './ProjectExplorer'
import { openWorkspaceFile } from './open'
import { useFiles } from './store'

vi.mock('./open', () => ({ openWorkspaceFile: vi.fn() }))
// jsdom has no layout measurements; render the visible virtual rows directly.
vi.mock('@tanstack/react-virtual', () => ({
  useVirtualizer: ({ count }: { count: number }) => ({
    getTotalSize: () => count * 32,
    getVirtualItems: () =>
      Array.from({ length: count }, (_, index) => ({
        key: index,
        index,
        start: index * 32,
        size: 32,
      })),
  }),
}))
const project: Project = {
  id: 'real',
  name: 'Real project',
  roots: ['/workspace/real/'],
  status: 'active',
  created_at: '',
  updated_at: '',
}
function mount(node: React.ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })
  return render(<QueryClientProvider client={client}>{node}</QueryClientProvider>)
}
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  localStorage.clear()
  useFiles.setState({ selected: '', changes: {} })
})

it('normalizes root paths and recognizes heavy directories', () => {
  expect(workspacePath('/workspace/', 'src/main.py')).toBe('/workspace/src/main.py')
  expect(workspacePath('/', 'report.pdf')).toBe('/report.pdf')
  expect(treeEntries(['src/', 'node_modules/…', 'report.pdf'])).toEqual([
    { path: 'src', folder: true, dim: false },
    { path: 'node_modules', folder: true, dim: true },
    { path: 'report.pdf', folder: false, dim: false },
  ])
})
it('loads directories lazily, follows pagination and opens absolute paths with change metadata', async () => {
  const change = {
    path: 'src/main.py',
    status: 'modified' as const,
    added: 1,
    removed: 0,
    hunks: [],
  }
  useFiles.setState({ changes: {}, selected: '' })
  const call = vi
    .spyOn(rpc, 'call')
    .mockResolvedValueOnce({ entries: ['src/', 'report.pdf'], next_cursor: '2' })
    .mockResolvedValueOnce({ entries: ['data.csv'], next_cursor: '' })
    .mockResolvedValueOnce({ entries: ['src/main.py'], next_cursor: '' })
  mount(<ProjectExplorer projectId={project.id} project={project} fileChanges={[change]} />)
  await screen.findByText('data.csv')
  expect(call).toHaveBeenCalledTimes(2)
  expect(call.mock.calls[1]?.[1]).toMatchObject({ cursor: '2', path: '' })
  fireEvent.click(screen.getByText('src'))
  const file = await screen.findByText('main.py')
  expect(call.mock.calls[2]?.[1]).toMatchObject({ project_id: 'real', path: 'src' })
  act(() => useFiles.setState({ selected: '/workspace/real/src/main.py' }))
  expect(file.closest('button')).toHaveClass('selected')
  expect(file.closest('button')?.querySelector('.changed-dot')).toBeTruthy()
  fireEvent.click(file)
  expect(openWorkspaceFile).toHaveBeenCalledWith('/workspace/real/src/main.py', change)
  fireEvent.click(screen.getByText('src'))
  expect(screen.queryByText('main.py')).toBeNull()
})
it('keeps the demo only when no projects are registered', async () => {
  vi.spyOn(rpc, 'call').mockResolvedValue({ projects: null })
  mount(<WorkspaceExplorer demo={<div>Offline tree</div>} />)
  expect(await screen.findByText('Offline tree')).toBeVisible()
})
it('restores the selected project and never replaces an empty real tree with demo files', async () => {
  localStorage.setItem('files-project', 'second')
  const call = vi
    .spyOn(rpc, 'call')
    .mockResolvedValueOnce({
      projects: [project, { ...project, id: 'second', name: 'Second project' }],
    })
    .mockResolvedValue({ entries: null, next_cursor: '' })
  mount(<WorkspaceExplorer demo={<div>Offline tree</div>} />)
  await waitFor(() =>
    expect(call).toHaveBeenCalledWith(
      'files.list',
      expect.objectContaining({ project_id: 'second' }),
      expect.anything(),
    ),
  )
  expect(screen.queryByText('Offline tree')).toBeNull()
  expect(screen.getByRole('combobox')).toBeVisible()
})
it('offers retry when a directory cannot be listed', async () => {
  const call = vi
    .spyOn(rpc, 'call')
    .mockRejectedValueOnce(new Error('offline'))
    .mockResolvedValue({ entries: ['retry.py'], next_cursor: '' })
  mount(<ProjectExplorer projectId={project.id} project={project} />)
  const alert = await screen.findByRole('alert')
  fireEvent.click(alert.querySelector('button')!)
  expect(await screen.findByText('retry.py')).toBeVisible()
  expect(call).toHaveBeenCalledTimes(2)
})
