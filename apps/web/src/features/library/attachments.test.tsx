import { afterEach, expect, it, vi } from 'vitest'
import { act, cleanup, fireEvent, render, renderHook, screen } from '@testing-library/react'
import '@/lib/i18n'
import * as api from './api'
import { useAttachments, useDocumentPolling } from './use-attachments'
import { documentUpdated, useLibraryStore } from './store'
import { AttachmentChips } from './Attachments'
import { attachmentText } from './attachment-text'
import { seedDocuments } from './seed'
import { Composer } from '@/features/chat/Composer'

const pending = { ...seedDocuments[1]!, id: 'upload-1' }
const ready = { ...pending, status: 'ready' as const, updated_at: '2026-11-01T00:00:00Z' }
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  vi.useRealTimers()
  useLibraryStore.setState({ documents: {} })
})
it('shows upload percentages, document processing, errors and removal controls', () => {
  const remove = vi.fn()
  render(
    <AttachmentChips
      items={[
        { key: 'upload', name: 'notes.txt', size: 1024, phase: 'uploading', percent: 37 },
        {
          key: 'doc',
          name: 'book.pdf',
          size: 2048,
          phase: 'document',
          percent: 100,
          document: pending,
        },
        {
          key: 'error',
          name: 'large.pdf',
          size: 100,
          phase: 'error',
          percent: 0,
          error: 'library_tooLarge',
        },
      ]}
      onRemove={remove}
    />,
  )
  expect(screen.getByText('Subiendo 37 %')).toBeVisible()
  expect(screen.getByRole('progressbar', { name: 'Subir documentos' })).toHaveAttribute(
    'value',
    '37',
  )
  expect(screen.getByText('Pendiente')).toBeVisible()
  expect(screen.getByRole('alert')).toHaveTextContent('64 MiB')
  fireEvent.click(screen.getByRole('button', { name: 'Quitar adjunto: notes.txt' }))
  expect(remove).toHaveBeenCalledWith('upload')
})
it('does not resurrect removed uploads and aborts on unmount', async () => {
  let finish!: (value: typeof pending) => void
  let signal: AbortSignal | undefined
  vi.spyOn(api, 'uploadDocument').mockImplementation((_file, options) => {
    signal = options?.signal
    return new Promise((resolve) => {
      finish = resolve
    })
  })
  const hook = renderHook(useAttachments)
  await act(async () => hook.result.current.add([new File(['a'], 'one.txt')]))
  const key = hook.result.current.items[0]!.key
  act(() => hook.result.current.remove(key))
  expect(signal?.aborted).toBe(true)
  await act(async () => finish(pending))
  expect(hook.result.current.items).toHaveLength(0)
  await act(async () => hook.result.current.add([new File(['b'], 'two.txt')]))
  hook.unmount()
  expect(signal?.aborted).toBe(true)
})
it('polls every two seconds, retries transient failures and stops at terminal status', async () => {
  vi.useFakeTimers()
  documentUpdated(pending)
  const call = vi
    .spyOn(api, 'libraryCall')
    .mockRejectedValueOnce(new Error('offline'))
    .mockResolvedValue({ document: ready })
  const hook = renderHook(() => useDocumentPolling([pending.id]))
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1999)
  })
  expect(call).not.toHaveBeenCalled()
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1)
  })
  expect(call).toHaveBeenCalledTimes(1)
  await act(async () => {
    await vi.advanceTimersByTimeAsync(2000)
  })
  expect(useLibraryStore.getState().documents[pending.id]?.status).toBe('ready')
  await act(async () => {
    await vi.advanceTimersByTimeAsync(4000)
  })
  expect(call).toHaveBeenCalledTimes(2)
  hook.unmount()
  expect(vi.getTimerCount()).toBe(0)
})
it('accepts document update events and rejects older progress', () => {
  documentUpdated(ready)
  documentUpdated(pending)
  expect(useLibraryStore.getState().documents[pending.id]).toEqual(ready)
})
it('blocks sending while ingesting, then includes titles and IDs and clears chips', async () => {
  vi.spyOn(api, 'uploadDocument').mockResolvedValue(pending)
  const send = vi.fn()
  const { container } = render(
    <Composer agent="Byte" typing={false} onSend={send} onStop={() => {}} />,
  )
  await act(async () =>
    fireEvent.change(container.querySelector('input[type=file]')!, {
      target: { files: [new File(['a'], 'notes.txt')] },
    }),
  )
  fireEvent.change(screen.getByRole('textbox'), { target: { value: 'Read this' } })
  expect(screen.getByRole('button', { name: 'Enviar mensaje' })).toBeDisabled()
  act(() => documentUpdated(ready))
  fireEvent.click(screen.getByRole('button', { name: 'Enviar mensaje' }))
  expect(send).toHaveBeenCalledWith(`Read this\nAdjuntos: ${ready.title} [document:${ready.id}]`)
  expect(screen.queryByText('notes.txt')).not.toBeInTheDocument()
  expect(attachmentText('', [ready])).toBe(`Attached: ${ready.title} [document:${ready.id}]`)
})
it('keeps failed chips removable and prevents sending unusable document references', async () => {
  vi.spyOn(api, 'uploadDocument').mockRejectedValue(new api.UploadError('tooLarge', 'Too big'))
  const send = vi.fn()
  const { container } = render(
    <Composer agent="Byte" typing={false} onSend={send} onStop={() => {}} />,
  )
  await act(async () =>
    fireEvent.change(container.querySelector('input[type=file]')!, {
      target: { files: [new File(['a'], 'bad.pdf')] },
    }),
  )
  fireEvent.change(screen.getByRole('textbox'), { target: { value: 'Hello' } })
  fireEvent.keyDown(screen.getByRole('textbox'), { key: 'Enter' })
  expect(send).not.toHaveBeenCalled()
  fireEvent.click(screen.getByRole('button', { name: 'Quitar adjunto: bad.pdf' }))
  fireEvent.keyDown(screen.getByRole('textbox'), { key: 'Enter' })
  expect(send).toHaveBeenCalledWith('Hello')
})
it('serializes repeated selections to keep file hashing memory bounded', async () => {
  let finish!: (value: typeof ready) => void
  const upload = vi
    .spyOn(api, 'uploadDocument')
    .mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve
        }),
    )
    .mockResolvedValue(ready)
  const hook = renderHook(useAttachments)
  await act(async () => {
    hook.result.current.add([new File(['a'], 'one.txt')])
    hook.result.current.add([new File(['b'], 'two.txt')])
  })
  expect(upload).toHaveBeenCalledTimes(1)
  await act(async () => finish(ready))
  expect(upload).toHaveBeenCalledTimes(2)
})

it('orders progress updates by time even when timestamp precision or offset differs', () => {
  documentUpdated({ ...pending, updated_at: '2026-11-01T00:00:00Z' })
  documentUpdated({ ...ready, updated_at: '2026-11-01T00:00:00.500Z' })
  documentUpdated({ ...pending, updated_at: '2026-11-01T01:00:00+01:00' })
  expect(useLibraryStore.getState().documents[pending.id]?.status).toBe('ready')
})
