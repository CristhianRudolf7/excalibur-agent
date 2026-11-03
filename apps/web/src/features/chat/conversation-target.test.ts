import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { rpc } from '@/lib/rpc/client'
import { chatStateKey, loadHistory, receiveEvent, sendMessage } from './live'
import { useChat } from './store'
import type { ConversationEvent } from '@/lib/rpc/contract'
const target = 'cnv_telegram_123'
beforeEach(() =>
  useChat.setState({ live: {}, online: {}, older: {}, historyError: {}, typing: {} }),
)
afterEach(() => vi.restoreAllMocks())
it('loads and sends in the linked conversation while isolating the regular web history', async () => {
  const call = vi
    .spyOn(rpc, 'call')
    .mockResolvedValueOnce({
      messages: [
        {
          id: 'telegram-message',
          conversation_id: target,
          role: 'assistant',
          text: 'Sign in on the web',
        },
      ],
    })
    .mockResolvedValueOnce({ msg_id: 'reply' })
  useChat
    .getState()
    .merge('excalibur', [{ id: 'web-message', sender: 'agent', text: 'Web history', time: '' }])
  await loadHistory('excalibur', undefined, undefined, target)
  await sendMessage(
    'excalibur',
    'Continue in this conversation',
    [],
    undefined,
    undefined,
    undefined,
    target,
  )
  expect(call.mock.calls[0]![1]).toMatchObject({ conversation_id: target })
  expect(call.mock.calls[1]![1]).toMatchObject({ conversation_id: target })
  expect(
    useChat.getState().live[chatStateKey('excalibur', target)]?.map((message) => message.id),
  ).toEqual(['telegram-message', 'reply'])
  expect(useChat.getState().live.excalibur?.map((message) => message.id)).toEqual(['web-message'])
})
it('accepts only matching events in a linked conversation', () => {
  const event = (conversation_id?: string): ConversationEvent => ({
    id: 'event-1',
    seq: 1,
    at: '',
    type: 'message.created',
    payload: { conversation_id, message_id: 'message-1', role: 'assistant', text: 'Linked reply' },
  })
  receiveEvent('excalibur', event('cnv_web_excalibur'), target)
  receiveEvent('excalibur', event(), target)
  expect(useChat.getState().live[chatStateKey('excalibur', target)]).toBeUndefined()
  receiveEvent('excalibur', event(target), target)
  expect(useChat.getState().live[chatStateKey('excalibur', target)]?.[0]?.text).toBe('Linked reply')
  expect(useChat.getState().live.excalibur).toBeUndefined()
})
