import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { subscribeEvents } from './events'

import { FakeSocket } from '@/features/chat/fake-socket'
beforeEach(() => {
  vi.useFakeTimers()
  FakeSocket.peers = []
  vi.stubGlobal('WebSocket', FakeSocket)
})
afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})
it('subscribes, merges replay/live delivery once and replays safely after reconnect and daemon sequence resets', () => {
  const event = vi.fn(),
    ready = vi.fn(),
    offline = vi.fn()
  const stop = subscribeEvents({ event, ready, offline })
  const first = FakeSocket.peers[0]!
  first.onopen?.()
  expect(JSON.parse(first.send.mock.calls[0]![0])).toMatchObject({
    method: 'conversation.subscribe',
    params: { cursor: '' },
  })
  const one = { id: 'e1', seq: 1, payload: {}, type: 'message.created' }
  first.ack([one])
  first.frame({ jsonrpc: '2.0', method: 'event', params: one })
  first.frame({ jsonrpc: '2.0', method: 'event', params: { ...one, id: 'e2', seq: 2 } })
  expect(event).toHaveBeenCalledTimes(2)
  expect(ready).toHaveBeenCalledOnce()
  first.onclose?.()
  expect(offline).toHaveBeenCalledOnce()
  vi.advanceTimersByTime(1000)
  const second = FakeSocket.peers[1]!
  second.onopen?.()
  expect(JSON.parse(second.send.mock.calls[0]![0]).params.cursor).toBe('')
  second.ack([
    { ...one, id: 'e2', seq: 2 },
    { ...one, id: 'e3', seq: 3 },
  ])
  expect(event).toHaveBeenCalledTimes(3)
  second.onclose?.()
  vi.advanceTimersByTime(1000)
  const restarted = FakeSocket.peers[2]!
  restarted.ack([{ ...one, id: 'new-daemon-event', seq: 1 }])
  expect(event).toHaveBeenCalledTimes(4)
  stop()
  vi.advanceTimersByTime(60000)
  expect(FakeSocket.peers).toHaveLength(3)
})
it('retries missing methods and handshake timeouts, and cleans up', () => {
  const offline = vi.fn()
  const stop = subscribeEvents({ event: vi.fn(), ready: vi.fn(), offline })
  FakeSocket.peers[0]!.frame({ jsonrpc: '2.0', id: 'subscribe', error: { code: -32601 } })
  vi.advanceTimersByTime(1000)
  expect(FakeSocket.peers).toHaveLength(2)
  vi.advanceTimersByTime(10000)
  expect(offline).toHaveBeenCalledTimes(2)
  stop()
  vi.advanceTimersByTime(60000)
  expect(FakeSocket.peers).toHaveLength(2)
})

it('subscribes to a feature-specific event filter without changing the default chat filter', () => {
  const stop = subscribeEvents({
    types: ['whiteboard.opened', 'whiteboard.ops'],
    event: vi.fn(),
    ready: vi.fn(),
    offline: vi.fn(),
  })
  const peer = FakeSocket.peers[0]!
  peer.onopen?.()
  expect(JSON.parse(peer.send.mock.calls[0]![0]).params.types).toEqual([
    'whiteboard.opened',
    'whiteboard.ops',
  ])
  stop()
})

it('delivers every live-only frame even though they carry no sequence or identity', () => {
  const event = vi.fn()
  const stop = subscribeEvents({ event, ready: vi.fn(), offline: vi.fn() })
  const peer = FakeSocket.peers[0]!
  peer.onopen?.()
  peer.ack([{ id: 'e1', seq: 5, payload: {}, type: 'message.created' }])
  const frame = { id: '', seq: 0, type: 'robotlab.frame', payload: { session: 's', step: 1 } }
  peer.frame({ jsonrpc: '2.0', method: 'event', params: frame })
  peer.frame({
    jsonrpc: '2.0',
    method: 'event',
    params: { ...frame, payload: { session: 's', step: 2 } },
  })
  peer.frame({
    jsonrpc: '2.0',
    method: 'event',
    params: { ...frame, payload: { session: 's', step: 3 } },
  })
  expect(event.mock.calls.filter(([e]) => e.type === 'robotlab.frame')).toHaveLength(3)
  stop()
})
