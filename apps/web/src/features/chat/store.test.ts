import { beforeEach, expect, it } from 'vitest'
import { useChat } from './store'
import type { Message } from './types'

const message = (id: string): Message => ({ id, text: id, sender: 'agent', time: '' })
const ids = () => useChat.getState().live.byte?.map((row) => row.id)
beforeEach(() => {
  useChat.setState({ live: {}, online: {}, historyLoaded: {}, historyCursor: {} })
})

it('keeps every loaded message through disconnect, resubscribe and empty history refreshes', () => {
  const chat = useChat.getState()
  chat.merge('byte', [message('a'), message('b')], 'latest')
  chat.page('byte', 'older', undefined, 'b')
  const cached = useChat.getState().live.byte
  chat.connection('byte', true)
  chat.connection('byte', false)
  chat.startHistory('byte')
  expect(useChat.getState().live.byte).toBe(cached)
  chat.merge('byte', [], 'latest')
  chat.connection('byte', true)
  expect(ids()).toEqual(['a', 'b'])
  expect(useChat.getState().historyCursor.byte).toBe('b')
})

it('merges latest snapshots without moving paginated history or dropping optimistic messages', () => {
  const chat = useChat.getState()
  chat.merge('byte', ['b', 'c', 'd'].map(message), 'latest')
  chat.merge('byte', ['a', 'b'].map(message), true)
  chat.merge('byte', [{ ...message('pending'), sender: 'user', delivery: 'sending' }])
  chat.merge('byte', ['c', 'd', 'e'].map(message), 'latest')
  chat.merge('byte', ['c', 'd', 'e'].map(message), 'latest')
  expect(ids()).toEqual(['a', 'b', 'c', 'd', 'e', 'pending'])
  chat.merge('byte', [message('d')], 'latest')
  expect(ids()).toEqual(['a', 'b', 'c', 'd', 'e', 'pending'])
})

it('fills gaps before a replayed live message and appends non-overlapping catch-up pages once', () => {
  const chat = useChat.getState()
  chat.merge('byte', [message('a')], 'latest')
  chat.merge('byte', [message('e')])
  chat.merge('byte', ['b', 'c', 'd', 'e', 'e'].map(message), 'latest')
  expect(ids()).toEqual(['a', 'b', 'c', 'd', 'e'])
  chat.merge('byte', ['f', 'g'].map(message), 'latest')
  expect(ids()).toEqual(['a', 'b', 'c', 'd', 'e', 'f', 'g'])
  chat.merge('other', [message('other')], 'latest')
  expect(ids()).toEqual(['a', 'b', 'c', 'd', 'e', 'f', 'g'])
})
