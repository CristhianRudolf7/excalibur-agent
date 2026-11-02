import { afterEach, expect, it, vi } from 'vitest'
import { rpc } from '@/lib/rpc/client'
import { fileViewKind, openWorkspaceFile, registerPanelOpener } from './open'
import { useFiles } from './store'
import { useViewerStore } from '../viewer/store'
import { seedDocument, seedPages } from '../viewer/seeds'
import { useWorkbook } from '../spreadsheet/store'

afterEach(() => {
  vi.restoreAllMocks()
  registerPanelOpener(null)
  useViewerStore.getState().close()
})
it.each([
  ['src/code.ts', 'code'],
  ['REPORT.PDF', 'document'],
  ['deck.pptx', 'document'],
  ['budget.xlsx', 'sheet'],
  ['budget.xls', 'sheet'],
  ['data.csv', 'sheet'],
  ['data.tsv', 'sheet'],
  ['data.ods', 'sheet'],
  ['photo.png', 'image'],
  ['archive.zip', 'other'],
])('routes %s to %s', (path, kind) => expect(fileViewKind(path)).toBe(kind))
it('loads unknown text into Monaco and preserves change metadata', async () => {
  const open = vi.fn()
  registerPanelOpener(open)
  vi.spyOn(rpc, 'call').mockResolvedValue({
    path: 'real.ts',
    kind: 'text',
    content: 'export const real = 42',
    mime: 'text/plain',
    size: 22,
  })
  await openWorkspaceFile('real.ts', {
    path: 'real.ts',
    status: 'added',
    added: 1,
    removed: 0,
    hunks: [],
  })
  expect(useFiles.getState().selected).toBe('real.ts')
  expect(useFiles.getState().changes['real.ts']?.content).toContain('real = 42')
  expect(open).toHaveBeenLastCalledWith('files')
})
it('opens a returned document ID', async () => {
  const open = vi.fn()
  registerPanelOpener(open)
  vi.spyOn(rpc, 'call').mockResolvedValue({
    path: 'report.pdf',
    kind: 'document',
    document_id: 'real-document',
    mime: 'application/pdf',
    size: 12,
  })
  await openWorkspaceFile('report.pdf')
  expect(useViewerStore.getState().target?.documentId).toBe('real-document')
  expect(open).toHaveBeenLastCalledWith('document')
})
it('loads the real workbook before opening the sheet panel', async () => {
  const open = vi.fn()
  registerPanelOpener(open)
  vi.spyOn(rpc, 'call')
    .mockResolvedValueOnce({
      path: 'budget.xlsx',
      kind: 'sheet',
      document_id: 'real-sheet',
      mime: '',
      size: 12,
    })
    .mockResolvedValueOnce({ document: seedDocument('demo-xlsx') })
    .mockResolvedValueOnce({ document: seedDocument('demo-xlsx'), pages: seedPages('demo-xlsx') })
  await openWorkspaceFile('budget.xlsx')
  expect(useWorkbook.getState().sheets[0]?.store.getState().cells.B2).toBe(42)
  expect(open).toHaveBeenLastCalledWith('sheet')
})
it('shows unsupported and recoverable errors', async () => {
  const call = vi
    .spyOn(rpc, 'call')
    .mockResolvedValueOnce({ path: 'file.bin', kind: 'binary', mime: '', size: 12 })
    .mockRejectedValueOnce(new Error('outside workspace'))
  await openWorkspaceFile('file.bin')
  expect(useViewerStore.getState().workspace?.status).toBe('unsupported')
  await openWorkspaceFile('missing.pdf')
  expect(useViewerStore.getState().workspace).toEqual({ path: 'missing.pdf', status: 'error' })
  expect(call).toHaveBeenCalledTimes(2)
})
it('keeps the latest file when an older request finishes later', async () => {
  let finish!: (value: unknown) => void
  vi.spyOn(rpc, 'call')
    .mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve
        }),
    )
    .mockResolvedValueOnce({ path: 'new.bin', kind: 'binary', mime: '', size: 1 })
  const first = openWorkspaceFile('old.pdf')
  await vi.waitFor(() => expect(finish).toBeTypeOf('function'))
  await openWorkspaceFile('new.bin')
  finish({ path: 'old.pdf', kind: 'document', document_id: 'old', mime: '', size: 1 })
  await first
  expect(useViewerStore.getState().workspace?.path).toBe('new.bin')
})

it('uses existing change content without an RPC and preserves deleted previews', async () => {
  const call = vi.spyOn(rpc, 'call')
  const open = vi.fn()
  registerPanelOpener(open)
  await openWorkspaceFile('deleted.ts', {
    path: 'deleted.ts',
    status: 'deleted',
    added: 0,
    removed: 1,
    hunks: [{ old_start: 1, old_lines: 1, new_start: 0, new_lines: 0, lines: ['-removed'] }],
  })
  expect(call).not.toHaveBeenCalled()
  expect(useFiles.getState().changes['deleted.ts']?.status).toBe('deleted')
  expect(open).toHaveBeenLastCalledWith('files')
})
it('shows a loading state and opens workspace blob images', async () => {
  let finish!: (value: unknown) => void
  vi.spyOn(rpc, 'call').mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        finish = resolve
      }),
  )
  const pending = openWorkspaceFile('image.png')
  expect(useViewerStore.getState().workspace).toEqual({ status: 'loading', path: 'image.png' })
  finish({
    path: 'image.png',
    kind: 'image',
    mime: 'image/png',
    blob_url: `/blobs/${'a'.repeat(64)}`,
    size: 100,
  })
  await pending
  expect(useViewerStore.getState().workspace?.status).toBe('image')
})

it('stores loaded text separately from conversation diffs', async () => {
  vi.spyOn(rpc, 'call').mockResolvedValue({
    path: 'unchanged.txt',
    kind: 'text',
    content: 'Original content',
    mime: 'text/plain',
    size: 16,
  })
  await openWorkspaceFile('unchanged.txt')
  expect(useFiles.getState().contents['unchanged.txt']).toBe('Original content')
  expect(useFiles.getState().changes['unchanged.txt']).toBeUndefined()
})
