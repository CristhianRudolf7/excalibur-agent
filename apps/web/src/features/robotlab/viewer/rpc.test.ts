import { expect, it, vi } from 'vitest'
const mocks = vi.hoisted(() => ({ call: vi.fn(), subscribe: vi.fn(), stop: vi.fn() }))
vi.mock('@/lib/rpc/client', () => ({ rpc: { call: mocks.call } }))
vi.mock('@/lib/rpc/events', () => ({ subscribeEvents: mocks.subscribe }))
import { subscribeFrames, viewerCall } from './rpc'
import { frameFixture } from './fixtures.test-support'
it('delegates RPCs to the existing transport and subscribes only to valid v2 frames', async () => {
  mocks.call.mockResolvedValue({ scene: null })
  const controller = new AbortController()
  await viewerCall('robotlab.session.scene', { session: 's' }, { signal: controller.signal })
  expect(mocks.call).toHaveBeenCalledWith(
    'robotlab.session.scene',
    { session: 's' },
    { signal: controller.signal },
  )
  mocks.subscribe.mockReturnValue(mocks.stop)
  const receive = vi.fn()
  const stop = subscribeFrames(receive)
  const options = mocks.subscribe.mock.calls[0]![0]
  expect(options.types).toEqual(['robotlab.frame'])
  options.event({ type: 'robotlab.log', payload: frameFixture() })
  options.event({ type: 'robotlab.frame', payload: { session: 's', poses: [[0, 0, 0]] } })
  options.event({ type: 'robotlab.frame', payload: frameFixture() })
  expect(receive).toHaveBeenCalledExactlyOnceWith(frameFixture())
  stop()
  expect(mocks.stop).toHaveBeenCalledOnce()
})
