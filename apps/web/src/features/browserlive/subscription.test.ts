import { afterEach, expect, it, vi } from 'vitest'
import { FakeSocket } from '@/features/chat/fake-socket'
import { subscribeEvents } from '@/lib/rpc/events'
afterEach(() => vi.unstubAllGlobals())
it('marks the bounded bus replay separately from fresh delivery on each socket', () => {
  FakeSocket.peers = []
  vi.stubGlobal('WebSocket', FakeSocket)
  const event = vi.fn()
  const stop = subscribeEvents({
    types: ['browser.session'],
    event,
    ready: vi.fn(),
    offline: vi.fn(),
  })
  const peer = FakeSocket.peers[0]!
  peer.onopen?.()
  expect(JSON.parse(peer.send.mock.calls[0]![0]).params.cursor).toBe('')
  const replay = { id: 'old', seq: 1, type: 'browser.session', payload: {} }
  peer.ack([replay])
  const fresh = { ...replay, id: 'fresh', seq: 2 }
  peer.frame({ jsonrpc: '2.0', method: 'event', params: fresh })
  expect(event).toHaveBeenNthCalledWith(1, replay, true)
  expect(event).toHaveBeenNthCalledWith(2, fresh, false)
  stop()
})
