import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { act, cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import '@/lib/i18n'
import { Library } from './Library'
import * as api from './api'
import { seedDocuments } from './seed'
import { useLibraryStore } from './store'
import { useViewerStore } from '@/features/viewer/store'

beforeEach(() => {
  vi.useFakeTimers()
  HTMLDialogElement.prototype.showModal = vi.fn(function (this: HTMLDialogElement) {
    this.setAttribute('open', '')
  })
  HTMLDialogElement.prototype.close = vi.fn()
})
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  vi.useRealTimers()
  useLibraryStore.setState({ documents: {} })
  useViewerStore.getState().close()
})
async function settle() {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(151)
  })
  await act(async () => {
    await vi.advanceTimersByTimeAsync(300)
  })
}
it('lists every state with progress, warnings, pages, dates and original downloads', async () => {
  const call = vi
    .spyOn(api, 'libraryCall')
    .mockResolvedValue({ documents: seedDocuments, next_cursor: '' })
  render(<Library />)
  await settle()
  for (const label of ['Listo', 'Pendiente', 'Procesando', 'Parcial', 'Error', 'Cancelado'])
    expect(screen.getByText(label)).toBeVisible()
  expect(screen.getByRole('progressbar', { name: 'Procesando' })).toHaveAttribute('value', '3')
  expect(screen.getByText('Parcial')).toHaveAttribute(
    'title',
    'Some scanned pages could not be extracted.',
  )
  expect(screen.getByText('read: The original file could not be read.')).toBeVisible()
  expect(screen.getAllByText(/8 páginas/)).toHaveLength(1)
  expect(screen.getAllByRole('link', { name: 'Descargar original' })[0]).toHaveAttribute(
    'href',
    `/blobs/${'0'.repeat(64)}`,
  )
  fireEvent.click(screen.getAllByRole('button', { name: 'Abrir' })[0]!)
  expect(useViewerStore.getState().target).toEqual({ documentId: seedDocuments[0]!.id })
  expect(call).toHaveBeenCalledWith(
    'library.list',
    { query: '', limit: 50 },
    { signal: expect.any(AbortSignal) },
  )
})
it('searches and displays an empty state for no matching documents', async () => {
  const call = vi
    .spyOn(api, 'libraryCall')
    .mockResolvedValue({ documents: seedDocuments, next_cursor: '' })
  render(<Library />)
  await settle()
  call.mockResolvedValue({ documents: [], next_cursor: '' })
  fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'missing' } })
  await settle()
  expect(screen.getByText('No hay documentos.')).toBeVisible()
  expect(call).toHaveBeenLastCalledWith(
    'library.list',
    { query: 'missing', limit: 50 },
    { signal: expect.any(AbortSignal) },
  )
})
it('shows explicit offline demo data without destructive or download actions', async () => {
  vi.spyOn(api, 'libraryCall').mockRejectedValue(new Error('offline'))
  render(<Library />)
  await settle()
  expect(screen.getByText(/Vista de ejemplo/)).toBeVisible()
  expect(screen.queryByRole('link', { name: 'Descargar original' })).not.toBeInTheDocument()
  for (const button of screen.getAllByRole('button', { name: 'Eliminar documento' }))
    expect(button).toBeDisabled()
  expect(screen.getByText('Citation guide')).toBeVisible()
})
it('confirms deletion with the backup notice and removes only the selected document', async () => {
  const call = vi
    .spyOn(api, 'libraryCall')
    .mockResolvedValue({ documents: seedDocuments, next_cursor: '' })
  render(<Library />)
  await settle()
  fireEvent.click(screen.getAllByRole('button', { name: 'Eliminar documento' })[0]!)
  expect(screen.getByRole('dialog')).toHaveTextContent(
    'Las copias de seguridad pueden conservar el original.',
  )
  expect(call).toHaveBeenCalledTimes(1)
  call.mockResolvedValue({ deleted: true, notice: 'Deleted' })
  await act(async () =>
    fireEvent.click(
      within(screen.getByRole('dialog')).getByRole('button', { name: 'Eliminar documento' }),
    ),
  )
  expect(call).toHaveBeenCalledWith('library.delete', { id: seedDocuments[0]!.id })
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  expect(screen.queryByText('Citation guide')).not.toBeInTheDocument()
  expect(screen.getByText('Research report')).toBeVisible()
})
it('re-ingests failed documents using their original digest and metadata', async () => {
  const call = vi
    .spyOn(api, 'libraryCall')
    .mockResolvedValue({ documents: seedDocuments, next_cursor: '' })
  render(<Library />)
  await settle()
  const failed = seedDocuments[4]!
  call.mockResolvedValue({
    document: { ...failed, status: 'pending', updated_at: '2026-11-02T00:00:00Z' },
    existing: false,
  })
  await act(async () =>
    fireEvent.click(screen.getByRole('button', { name: 'Reintentar extracción' })),
  )
  expect(call).toHaveBeenCalledWith('library.ingest', {
    blob_digest: failed.blob_digest,
    name: failed.source_name,
    mime: failed.mime,
  })
  expect(screen.queryByRole('button', { name: 'Reintentar extracción' })).not.toBeInTheDocument()
})
it('paginates without duplicating existing documents', async () => {
  const call = vi
    .spyOn(api, 'libraryCall')
    .mockResolvedValue({ documents: [seedDocuments[0]!], next_cursor: 'next' })
  render(<Library />)
  await settle()
  call.mockResolvedValue({ documents: [seedDocuments[0]!, seedDocuments[1]!], next_cursor: '' })
  await act(async () => fireEvent.click(screen.getByRole('button', { name: 'Cargar más' })))
  expect(call).toHaveBeenCalledWith('library.list', { query: '', cursor: 'next', limit: 50 })
  expect(screen.getAllByText('Citation guide')).toHaveLength(1)
  expect(screen.getByText('Meeting notes')).toBeVisible()
})
it('uploads dropped files through the same attachment flow', async () => {
  vi.spyOn(api, 'libraryCall').mockResolvedValue({ documents: [], next_cursor: '' })
  const upload = vi.spyOn(api, 'uploadDocument').mockResolvedValue(seedDocuments[0]!)
  render(<Library />)
  await settle()
  const file = new File(['content'], 'guide.pdf', { type: 'application/pdf' })
  await act(async () =>
    fireEvent.drop(screen.getByText(/Arrastra documentos/).parentElement!, {
      dataTransfer: { files: [file] },
    }),
  )
  expect(upload).toHaveBeenCalledWith(
    file,
    expect.objectContaining({ signal: expect.any(AbortSignal) }),
  )
  expect(screen.getByText('Citation guide')).toBeVisible()
})

it('keeps uploaded documents in the list after dismissing their upload chips', async () => {
  vi.spyOn(api, 'libraryCall').mockResolvedValue({ documents: [], next_cursor: '' })
  vi.spyOn(api, 'uploadDocument').mockResolvedValue(seedDocuments[0]!)
  render(<Library />)
  await settle()
  await act(async () =>
    fireEvent.change(screen.getByLabelText('Subir documentos'), {
      target: { files: [new File(['a'], 'guide.pdf')] },
    }),
  )
  fireEvent.click(screen.getByRole('button', { name: 'Quitar adjunto: guide.pdf' }))
  expect(screen.queryByText('guide.pdf')).not.toBeInTheDocument()
  expect(screen.getByText('Citation guide')).toBeVisible()
})
