import { act, cleanup, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import i18n from '@/lib/i18n'
import { rpc } from '@/lib/rpc/client'
import type { ConversationEvent } from '@/lib/rpc/contract'
import { useChat } from '@/features/chat/store'
import {
  fromWire,
  conversationId,
  sendMessage,
  loadHistory,
  receiveEvent,
} from '@/features/chat/live'
import { Chat } from '@/features/chat/Chat'
import { defaultAgent, useAgents } from './store'

vi.mock('@/lib/rpc/events', () => ({ subscribeEvents: () => () => {} }))
vi.mock('@tanstack/react-virtual', () => ({
  useVirtualizer: ({ count }: { count: number }) => ({
    getTotalSize: () => count * 100,
    getVirtualItems: () =>
      Array.from({ length: count }, (_, index) => ({ index, start: index * 100 })),
    measureElement: () => {},
    scrollToIndex: () => {},
  }),
}))
const ada = { ...defaultAgent, id: 'ada', name: 'Ada', conversation_id: 'cnv_agent_ada' }
const sam = {
  ...defaultAgent,
  id: 'sam',
  name: 'Sam',
  favorite: false,
  conversation_id: 'cnv_agent_sam',
}
beforeEach(async () => {
  await i18n.changeLanguage('en')
  useAgents.setState({ agents: [ada, sam], favorite_id: ada.id, selectedId: undefined })
  useChat.setState({ live: {}, online: {}, historyError: {}, typing: {}, activity: {} })
})
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})
const event = (conversation: string, text: string, extra = {}): ConversationEvent => ({
  seq: 1,
  id: text,
  at: '',
  type: 'message.created',
  payload: {
    message_id: text,
    conversation_id: conversation,
    role: 'user',
    text,
    ...extra,
  },
})
it('uses the server conversation ID for history, sends and incoming messages, keeping histories separate', async () => {
  const call = vi.spyOn(rpc, 'call').mockResolvedValueOnce({ messages: [] })
  await loadHistory('ada')
  expect(call).toHaveBeenCalledWith(
    'conversation.history',
    { conversation_id: 'cnv_agent_ada', limit: 50 },
    expect.anything(),
  )
  call.mockResolvedValueOnce({ msg_id: 'sent' })
  await sendMessage('sam', 'Sam only')
  expect(call).toHaveBeenLastCalledWith(
    'conversation.send',
    expect.objectContaining({ conversation_id: 'cnv_agent_sam' }),
    expect.anything(),
  )
  receiveEvent('ada', event('cnv_agent_sam', 'Other chat'))
  receiveEvent('sam', event('cnv_agent_sam', 'Sam reply'))
  expect(useChat.getState().live.ada).toEqual([])
  expect(useChat.getState().live.sam?.map((message) => message.text)).toEqual([
    'Sam only',
    'Sam reply',
  ])
  expect(conversationId('excalibur')).toBe('cnv_web_excalibur')
})
it('renders relays as user messages with the sender name, including archived senders', async () => {
  vi.spyOn(rpc, 'call').mockResolvedValue({ messages: [] })
  useAgents.setState({ agents: [ada, { ...sam, archived: true }] })
  render(<Chat agentId="ada" onFile={() => {}} />)
  await waitFor(() => expect(useChat.getState().live.ada).toEqual([]))
  act(() => {
    receiveEvent('ada', event('cnv_agent_ada', 'Please review this', { relayed_by: 'sam' }))
    receiveEvent('ada', event('cnv_agent_sam', 'Private elsewhere'))
  })
  expect(await screen.findByText('sent by Sam')).toBeInTheDocument()
  expect(screen.getByText('Please review this').closest('article')).toHaveClass('message-user')
  expect(screen.queryByText('Private elsewhere')).toBeNull()
  expect(
    fromWire({
      id: 'relay',
      conversation_id: ada.conversation_id,
      role: 'user',
      text: 'History relay',
      relayed_by: 'sam',
    }).relayedBy,
  ).toBe('sam')
})
it('does not mistake a relayed message for a pending user echo with the same text', () => {
  useChat
    .getState()
    .merge('ada', [{ id: 'pending', sender: 'user', delivery: 'sending', text: 'Hello', time: '' }])
  receiveEvent('ada', event(ada.conversation_id, 'Hello', { relayed_by: 'sam' }))
  expect(useChat.getState().live.ada).toHaveLength(2)
})
it('acknowledges a newer unread event even when its count stays the same', async () => {
  useAgents.setState({ agents: [{ ...ada, unread: 1 }, sam] })
  const acknowledgements: (() => void)[] = []
  const call = vi.spyOn(rpc, 'call').mockImplementation(async (method) => {
    if (method === 'agents.mark_read') {
      await new Promise<void>((resolve) => acknowledgements.push(resolve))
      return { unread: 0 } as never
    }
    return { messages: [] } as never
  })
  render(<Chat agentId="ada" onFile={() => {}} />)
  await waitFor(() => expect(acknowledgements).toHaveLength(1))
  act(() => {
    useAgents.getState().unread({
      agent_id: ada.id,
      unread: 1,
      last_preview: 'A response arrived during the acknowledgement',
      last_message_at: '2026-11-03T12:00:00Z',
    })
  })
  try {
    await waitFor(() => expect(acknowledgements).toHaveLength(2))
    await act(async () => acknowledgements[0]!())
    expect(useAgents.getState().agents[0]!.unread).toBe(1)
    await act(async () => acknowledgements[1]!())
    expect(useAgents.getState().agents[0]!.unread).toBe(0)
    expect(call.mock.calls.filter(([method]) => method === 'agents.mark_read')).toHaveLength(2)
  } finally {
    await act(async () => acknowledgements.forEach((resolve) => resolve()))
  }
})
it('routes scoped progress and questions to their agent, and system events to the favorite', () => {
  const progress: ConversationEvent = {
    seq: 1,
    id: 'p',
    at: '',
    type: 'run.progress',
    payload: { state: 'running', conversation_id: sam.conversation_id },
  }
  receiveEvent('ada', progress)
  receiveEvent('sam', progress)
  expect(useChat.getState().typing.ada).toBeUndefined()
  expect(useChat.getState().typing.sam).toBe(true)
  const notice: ConversationEvent = {
    ...progress,
    type: 'notice.posted',
    payload: { notice_id: 'n', content: 'System notice' },
  }
  receiveEvent('ada', notice)
  receiveEvent('sam', notice)
  expect(useChat.getState().live.ada?.[0]?.text).toBe('System notice')
  expect(useChat.getState().live.sam).toBeUndefined()
})
