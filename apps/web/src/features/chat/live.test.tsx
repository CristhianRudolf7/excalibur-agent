import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import '@/lib/i18n'
import { rpc, RpcError, TransportError } from '@/lib/rpc/client'
import type { ConversationEvent, ConversationMessage } from '@/lib/rpc/contract'
import { useLibraryStore } from '@/features/library/store'
import { seedDocuments, seedSources } from '@/features/library/seed'
import { useViewerStore } from '@/features/viewer/store'
import MarkdownBody from '@/components/markdown/MarkdownBody'
import { Chat } from './Chat'
import { Composer } from './Composer'
import * as attachmentHooks from '@/features/library/use-attachments'
import { useChat } from './store'
import {
  connectConversation,
  conversationId,
  loadHistory,
  receiveEvent,
  sendMessage,
  liveChatEnabled,
} from './live'
import { FakeSocket } from './fake-socket'

// Credential forms have their own query and security tests.
vi.mock('@/features/credentials/CredentialRequests', () => ({ CredentialRequests: () => null }))
vi.mock('@tanstack/react-virtual', () => ({
  useVirtualizer: ({ count }: { count: number }) => ({
    getTotalSize: () => count * 100,
    getVirtualItems: () =>
      Array.from({ length: count }, (_, index) => ({ index, start: index * 100 })),
    measureElement: () => {},
    scrollToIndex: () => {},
  }),
}))
vi.mock('@/components/markdown/Markdown', () => ({
  Markdown: (props: { children: string; sources?: typeof seedSources }) => (
    <MarkdownBody {...props} />
  ),
}))
const wire = (id: string, text = id): ConversationMessage => ({
  id,
  text,
  conversation_id: conversationId('byte'),
  role: 'assistant',
})
const event = (message: ConversationMessage): ConversationEvent => ({
  id: message.id,
  seq: 1,
  type: 'message.created',
  at: '',
  payload: { ...message, message_id: message.id },
})
beforeEach(() => {
  useChat.setState({
    live: {},
    online: {},
    connectedOnce: {},
    historyBefore: {},
    older: {},
    historyError: {},
    historyLoaded: {},
    historyCursor: {},
    typing: {},
  })
  FakeSocket.peers = []
  vi.stubGlobal('WebSocket', FakeSocket)
})
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  vi.useRealTimers()
})
it('sends optimistically with document IDs, deduplicates an early echo and retries the same client ID', async () => {
  let resolve!: (receipt: { msg_id: string }) => void
  const call = vi.spyOn(rpc, 'call').mockImplementationOnce(
    () =>
      new Promise((done) => {
        resolve = done
      }) as never,
  )
  const pending = sendMessage('byte', 'Read this', ['doc-1'])
  expect(useChat.getState().live.byte?.[0]).toMatchObject({
    text: 'Read this',
    delivery: 'sending',
  })
  expect(call).toHaveBeenCalledWith(
    'conversation.send',
    expect.objectContaining({ conversation_id: 'cnv_agent_byte', attachments: ['doc-1'] }),
    expect.objectContaining({ timeoutMs: 30_000 }),
  )
  receiveEvent('byte', event({ ...wire('msg-1', 'Read this'), role: 'user' }))
  resolve({ msg_id: 'msg-1' })
  await pending
  expect(useChat.getState().live.byte).toHaveLength(1)
  call.mockRejectedValueOnce(new RpcError(-32099, 'Try again'))
  await sendMessage('byte', 'Again')
  const failed = useChat.getState().live.byte!.find((message) => message.delivery === 'failed')!
  expect(failed.error).toBe('Try again')
  call.mockResolvedValueOnce({ msg_id: 'msg-2' })
  await sendMessage('byte', failed.text, failed.attachments, failed)
  expect(call.mock.calls[1]![1]).toEqual(call.mock.calls[2]![1])
  expect(useChat.getState().live.byte!.every((message) => !message.delivery)).toBe(true)
})
it('paginates history and merges overlapping live events without duplicates or cross-conversation messages', async () => {
  const rows = Array.from({ length: 50 }, (_, i) => wire(`msg-${String(i + 50).padStart(3, '0')}`))
  const call = vi.spyOn(rpc, 'call').mockResolvedValueOnce({ messages: rows })
  receiveEvent('byte', event(rows[49]!))
  receiveEvent('byte', event({ ...wire('other'), conversation_id: 'cnv_agent_atlas' }))
  await loadHistory('byte')
  expect(useChat.getState().live.byte).toHaveLength(50)
  expect(useChat.getState().older.byte).toBe('msg-050')
  call.mockResolvedValueOnce({ messages: [wire('msg-049'), rows[0]!] })
  await loadHistory('byte', useChat.getState().older.byte)
  expect(call).toHaveBeenLastCalledWith(
    'conversation.history',
    { conversation_id: 'cnv_agent_byte', before: 'msg-050', limit: 50 },
    { signal: undefined },
  )
  expect(useChat.getState().live.byte).toHaveLength(51)
  expect(useChat.getState().live.byte![0]!.id).toBe('msg-049')
  expect(useChat.getState().older.byte).toBeUndefined()
})
it('renders event sources and forwards library updates; global notices/progress belong to master', () => {
  receiveEvent('byte', event({ ...wire('msg-cited', 'Evidence [1]'), sources: seedSources }))
  const message = useChat.getState().live.byte![0]!
  render(<MarkdownBody sources={message.sources}>{message.text}</MarkdownBody>)
  fireEvent.click(screen.getByRole('button', { name: 'Fuente 1' }))
  expect(useViewerStore.getState().target?.documentId).toBe(seedSources[0]!.document_id)
  const document = seedDocuments[0]!
  receiveEvent('byte', { ...event(wire('')), type: 'library.document_updated', payload: document })
  expect(useLibraryStore.getState().documents[document.id]).toEqual(document)
  const notice = {
    ...event(wire('')),
    type: 'notice.posted',
    payload: { notice_id: 'n1', content: 'Finished reading' },
  }
  receiveEvent('byte', notice)
  receiveEvent('excalibur', notice)
  receiveEvent('excalibur', notice)
  expect(useChat.getState().live.excalibur).toHaveLength(1)
  receiveEvent('excalibur', { ...notice, type: 'run.progress', payload: { state: 'running' } })
  expect(useChat.getState().typing.excalibur).toBe(true)
  receiveEvent('excalibur', {
    ...notice,
    type: 'run.state_changed',
    payload: { state: 'completed' },
  })
  expect(useChat.getState().typing.excalibur).toBe(false)
})
it('keeps the first-load skeleton through transient failures and sends after recovery', async () => {
  if (!liveChatEnabled) return
  vi.useFakeTimers()
  const call = vi
    .spyOn(rpc, 'call')
    .mockRejectedValueOnce(new TransportError('Offline', 'network'))
    .mockRejectedValueOnce(new TransportError('Offline', 'network'))
    .mockResolvedValueOnce({ messages: [wire('msg-live', 'Real history')] })
  render(<Chat agentId="byte" onFile={() => {}} />)
  await act(async () => {
    FakeSocket.peers[0]!.ack()
    await vi.advanceTimersByTimeAsync(200)
  })
  expect(screen.getByRole('status', { name: 'Cargando mensajes' })).toBeVisible()
  expect(screen.queryByText('Reconectando…')).not.toBeInTheDocument()
  expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  await act(async () => vi.advanceTimersByTimeAsync(1400))
  expect(screen.getByText('Real history')).toBeInTheDocument()
  expect(useChat.getState().online.byte).toBe(true)
  call.mockResolvedValueOnce({ msg_id: 'msg-sent' })
  fireEvent.change(screen.getByRole('textbox'), { target: { value: 'Hello daemon' } })
  await act(async () => fireEvent.keyDown(screen.getByRole('textbox'), { key: 'Enter' }))
  expect(call).toHaveBeenCalledWith(
    'conversation.send',
    expect.objectContaining({ text: 'Hello daemon', attachments: [] }),
    expect.objectContaining({ timeoutMs: 30_000 }),
  )
})
it('reports history errors only after retries, preserves messages, and recovers via the bounded probe', async () => {
  if (!liveChatEnabled) return
  vi.useFakeTimers()
  const call = vi.spyOn(rpc, 'call').mockRejectedValue(new RpcError(-32601, 'Missing method'))
  render(<Chat agentId="byte" onFile={() => {}} />)
  await act(async () => {
    FakeSocket.peers[0]!.ack()
    await vi.advanceTimersByTimeAsync(1500)
  })
  expect(call).toHaveBeenCalledTimes(3)
  expect(screen.getByRole('alert')).toHaveTextContent('No se pudo cargar el historial.')
  expect(screen.getByRole('button', { name: 'Reintentar' })).toBeVisible()
  expect(screen.queryByText('Reconectando…')).not.toBeInTheDocument()
  call.mockResolvedValue({ messages: [wire('kept', 'Known history')] })
  await act(async () => vi.advanceTimersByTimeAsync(8500))
  expect(screen.getByText('Known history')).toBeVisible()
  expect(useChat.getState().online.byte).toBe(true)
  await act(async () => FakeSocket.peers[0]!.onclose?.())
  expect(screen.getByText('Known history')).toBeVisible()
  expect(screen.getByText('Reconectando…')).toBeVisible()
  cleanup()
  const calls = call.mock.calls.length
  await vi.advanceTimersByTimeAsync(60000)
  expect(call).toHaveBeenCalledTimes(calls)
})

it('honors the chat switch without opening a socket when disabled', () => {
  const call = vi.spyOn(rpc, 'call')
  const stop = connectConversation('byte')
  expect(FakeSocket.peers).toHaveLength(liveChatEnabled ? 1 : 0)
  expect(call).toHaveBeenCalledTimes(liveChatEnabled ? 1 : 0)
  stop()
})

it('keeps pagination retryable after a server error', async () => {
  vi.useFakeTimers()
  vi.spyOn(rpc, 'call').mockRejectedValue(new RpcError(-32099, 'History busy'))
  useChat.getState().connection('byte', true)
  const loading = loadHistory('byte', 'msg-050')
  await vi.advanceTimersByTimeAsync(1500)
  await loading
  expect(useChat.getState().older.byte).toBe('msg-050')
  expect(useChat.getState().historyError.byte).toBe('History busy')
  expect(useChat.getState().online.byte).toBe(true)
})

it('renders a failed send with retry and receives cited assistant replies in chat', async () => {
  if (!liveChatEnabled) return
  const call = vi.spyOn(rpc, 'call').mockResolvedValueOnce({ messages: [] })
  render(<Chat agentId="byte" onFile={() => {}} />)
  await act(async () => FakeSocket.peers[0]!.ack())
  call.mockRejectedValueOnce(new RpcError(-32099, 'Send busy'))
  fireEvent.change(screen.getByRole('textbox'), { target: { value: 'Question' } })
  fireEvent.keyDown(screen.getByRole('textbox'), { key: 'Enter' })
  await screen.findByText('Send busy')
  call.mockResolvedValueOnce({ msg_id: 'msg-user' })
  fireEvent.click(screen.getByRole('button', { name: 'Reintentar' }))
  await waitFor(() => expect(screen.queryByText('Send busy')).not.toBeInTheDocument())
  call.mockResolvedValueOnce({ msg_id: 'msg-stop' })
  fireEvent.click(screen.getByRole('button', { name: 'Detener respuesta' }))
  await waitFor(() =>
    expect(call).toHaveBeenCalledWith(
      'conversation.send',
      expect.objectContaining({ text: '/stop' }),
      expect.objectContaining({ timeoutMs: 30_000 }),
    ),
  )
  expect(useChat.getState().typing.byte).toBe(false)
  act(() =>
    FakeSocket.peers[0]!.frame({
      jsonrpc: '2.0',
      method: 'event',
      params: event({ ...wire('msg-z', 'Answer [1]'), sources: seedSources }),
    }),
  )
  expect(screen.getByRole('button', { name: 'Fuente 1' })).toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Fuentes (2)' })).toBeInTheDocument()
  expect(useChat.getState().typing.byte).toBe(false)
})

it('passes ready composer document IDs separately from display text', () => {
  const document = seedDocuments[0]!
  const clear = vi.fn(),
    send = vi.fn()
  vi.spyOn(attachmentHooks, 'useAttachments').mockReturnValue({
    items: [
      {
        key: 'attachment',
        name: document.source_name,
        size: document.size,
        phase: 'document',
        percent: 100,
        document,
      },
    ],
    add: vi.fn(),
    remove: vi.fn(),
    clear,
  })
  render(
    <Composer
      agent="Byte"
      typing={false}
      onSend={() => {}}
      onSendDocuments={send}
      onStop={() => {}}
    />,
  )
  fireEvent.change(screen.getByRole('textbox'), { target: { value: 'Read this' } })
  fireEvent.keyDown(screen.getByRole('textbox'), { key: 'Enter' })
  expect(send).toHaveBeenCalledWith(expect.stringContaining('Read this'), [document.id])
  expect(clear).toHaveBeenCalledOnce()
})

it('reconciles the echo before the receipt without collapsing distinct repeated messages', async () => {
  let receipt!: (value: { msg_id: string }) => void
  vi.spyOn(rpc, 'call').mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        receipt = resolve
      }) as never,
  )
  const pending = sendMessage('excalibur', 'Repeat this')
  receiveEvent(
    'excalibur',
    event({
      ...wire('message-1', 'Repeat this'),
      conversation_id: conversationId('excalibur'),
      role: 'user',
    }),
  )
  expect(useChat.getState().live.excalibur).toHaveLength(1)
  expect(useChat.getState().live.excalibur?.[0]?.delivery).toBeUndefined()
  receipt({ msg_id: 'message-1' })
  await pending
  receiveEvent(
    'excalibur',
    event({
      ...wire('message-2', 'Repeat this'),
      conversation_id: conversationId('excalibur'),
      role: 'user',
    }),
  )
  expect(useChat.getState().live.excalibur).toHaveLength(2)
})

it('preserves file changes through history, live events and store merges', async () => {
  const changes = [
    {
      path: 'new.ts',
      status: 'added' as const,
      added: 1,
      removed: 0,
      content: 'new',
      hunks: [{ old_start: 0, old_lines: 0, new_start: 1, new_lines: 1, lines: ['+new'] }],
    },
  ]
  vi.spyOn(rpc, 'call').mockResolvedValueOnce({ messages: [{ ...wire('changed'), changes }] })
  await loadHistory('byte')
  expect(useChat.getState().live.byte?.[0]?.changes).toEqual(changes)
  receiveEvent('byte', event({ ...wire('changed', 'Updated answer'), changes }))
  expect(useChat.getState().live.byte).toHaveLength(1)
  expect(useChat.getState().live.byte?.[0]?.changes).toEqual(changes)
})

it('keeps classroom context on the single conversation and preserves it on retry', async () => {
  const call = vi.spyOn(rpc, 'call').mockRejectedValueOnce(new RpcError(-32099, 'Try again'))
  const context = { app: 'classroom', course_id: 'course-1', topic_id: 'topic-2' } as const
  await sendMessage('excalibur', 'Explain this', [], undefined, undefined, context)
  const failed = useChat.getState().live.excalibur![0]!
  call.mockResolvedValueOnce({ msg_id: 'retried' } as never)
  await sendMessage('excalibur', failed.text, [], failed)
  expect(call).toHaveBeenLastCalledWith(
    'conversation.send',
    expect.objectContaining({
      conversation_id: 'cnv_web_excalibur',
      context,
      client_msg_id: failed.clientMsgId,
    }),
    { timeoutMs: 30_000 },
  )
})

it('catches up across history pages after reconnect without clearing or reordering cached rows', async () => {
  vi.useFakeTimers()
  const initial = Array.from({ length: 50 }, (_, i) => wire(`m-${i}`))
  const missed = Array.from({ length: 70 }, (_, i) => wire(`m-${i + 50}`))
  const call = vi.spyOn(rpc, 'call').mockResolvedValue({ messages: initial })
  const stop = connectConversation('byte')
  FakeSocket.peers[0]!.ack()
  await vi.advanceTimersByTimeAsync(0)
  call.mockResolvedValueOnce({ messages: [wire('m-older')] })
  await loadHistory('byte', 'm-0')
  const cached = useChat.getState().live.byte
  FakeSocket.peers[0]!.onclose?.()
  expect(useChat.getState().live.byte).toBe(cached)
  await vi.advanceTimersByTimeAsync(1000)
  let release!: (value: { messages: ConversationMessage[] }) => void
  call.mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        release = resolve
      }) as never,
  )
  // Replay may already include the newest row, but catch-up must still reach
  // the history cursor from before the outage rather than stopping at replay.
  FakeSocket.peers[1]!.ack([event(missed.at(-1)!)])
  expect(useChat.getState().online.byte).toBe(false)
  expect(useChat.getState().live.byte?.slice(0, 51)).toEqual(cached)
  call.mockResolvedValueOnce({ messages: [...initial.slice(20), ...missed.slice(0, 20)] })
  release({ messages: missed.slice(20) })
  await vi.advanceTimersByTimeAsync(0)
  expect(useChat.getState().live.byte?.map((row) => row.id)).toEqual([
    'm-older',
    ...initial.map((row) => row.id),
    ...missed.map((row) => row.id),
  ])
  expect(call).toHaveBeenLastCalledWith(
    'conversation.history',
    { conversation_id: conversationId('byte'), limit: 50, before: 'm-70' },
    expect.objectContaining({ signal: expect.any(AbortSignal) }),
  )
  expect(useChat.getState().online.byte).toBe(true)
  expect(useChat.getState().older.byte).toBeUndefined()
  stop()
})

it('automatically retries offline sends with the same client ID when the stream reconnects', async () => {
  vi.useFakeTimers()
  const call = vi.spyOn(rpc, 'call').mockResolvedValue({ messages: [wire('known')] })
  const stop = connectConversation('byte')
  FakeSocket.peers[0]!.ack()
  await vi.advanceTimersByTimeAsync(0)
  FakeSocket.peers[0]!.onclose?.()
  call.mockRejectedValueOnce(new TransportError('Offline', 'network'))
  await sendMessage('byte', 'Queued question')
  const failed = useChat.getState().live.byte!.at(-1)!
  expect(failed.retryOnReconnect).toBe(true)
  await vi.advanceTimersByTimeAsync(1000)
  call.mockResolvedValueOnce({ msg_id: 'queued-receipt' })
  FakeSocket.peers[1]!.ack()
  await vi.advanceTimersByTimeAsync(0)
  const sends = call.mock.calls.filter(([method]) => method === 'conversation.send')
  expect(sends).toHaveLength(2)
  expect(sends[1]![1]).toEqual(sends[0]![1])
  expect(useChat.getState().live.byte?.filter((row) => row.text === 'Queued question')).toEqual([
    expect.objectContaining({ id: 'queued-receipt', delivery: undefined }),
  ])
  stop()
})

it('cancels history retry backoff when the conversation closes', async () => {
  vi.useFakeTimers()
  const call = vi.spyOn(rpc, 'call').mockRejectedValue(new TransportError('Offline', 'network'))
  const stop = connectConversation('byte')
  await vi.advanceTimersByTimeAsync(0)
  stop()
  await vi.advanceTimersByTimeAsync(60000)
  expect(call).toHaveBeenCalledOnce()
  expect(useChat.getState().historyError.byte).toBeUndefined()
})

it('keeps an empty first history in a skeleton until the stream is ready', async () => {
  vi.useFakeTimers()
  vi.spyOn(rpc, 'call').mockResolvedValue({ messages: [] })
  render(<Chat agentId="byte" onFile={() => {}} />)
  await act(async () => vi.advanceTimersByTimeAsync(200))
  expect(screen.getByRole('status', { name: 'Cargando mensajes' })).toBeVisible()
  expect(screen.queryByText('Reconectando…')).not.toBeInTheDocument()
  await act(async () => FakeSocket.peers[0]!.ack())
  expect(screen.queryByRole('status', { name: 'Cargando mensajes' })).not.toBeInTheDocument()
})

it('renders live messages as soon as they arrive while initial history is still pending', async () => {
  vi.useFakeTimers()
  let release!: (value: { messages: ConversationMessage[] }) => void
  vi.spyOn(rpc, 'call').mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        release = resolve
      }) as never,
  )
  render(<Chat agentId="byte" onFile={() => {}} />)
  await act(async () => vi.advanceTimersByTimeAsync(200))
  await act(async () => receiveEvent('byte', event(wire('newest', 'Live while loading'))))
  expect(screen.getByText('Live while loading')).toBeVisible()
  expect(screen.queryByRole('status', { name: 'Cargando mensajes' })).not.toBeInTheDocument()
  await act(async () => release({ messages: [wire('older', 'Earlier history')] }))
  expect(useChat.getState().live.byte?.map((row) => row.id)).toEqual(['older', 'newest'])
})
