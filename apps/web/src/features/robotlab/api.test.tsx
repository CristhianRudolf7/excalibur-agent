import { afterEach, expect, it, vi } from 'vitest'
import { labCall, onRobotEvent, sessionImage } from './api'
const mocks = vi.hoisted(() => ({ call: vi.fn(), subscribe: vi.fn(), stop: vi.fn() }))
vi.mock('@/lib/rpc/client', () => ({ rpc: { call: mocks.call } }))
vi.mock('@/lib/rpc/events', () => ({ subscribeEvents: mocks.subscribe }))
afterEach(() => vi.clearAllMocks())
it('uses the shared RPC transport with unchanged v2 methods and params', async () => {
  mocks.call.mockResolvedValue({ environments: [] })
  await labCall('robotlab.env.list', {})
  expect(mocks.call).toHaveBeenCalledWith('robotlab.env.list', {}, undefined)
})
it('shares one feature event subscription and releases it after the last listener', () => {
  mocks.subscribe.mockReturnValue(mocks.stop)
  const focus = vi.fn(),
    session = vi.fn()
  const stopFocus = onRobotEvent('robotlab.focus', focus)
  const stopSession = onRobotEvent('robotlab.session', session)
  expect(mocks.subscribe).toHaveBeenCalledTimes(1)
  const options = mocks.subscribe.mock.calls[0]![0]
  expect(options.types).toEqual(
    expect.arrayContaining(['robotlab.focus', 'robotlab.session', 'robotlab.log']),
  )
  const payload = { session: 'session-1' }
  options.event({ type: 'robotlab.focus', payload })
  expect(focus).toHaveBeenCalledWith(payload)
  expect(session).not.toHaveBeenCalled()
  stopFocus()
  expect(mocks.stop).not.toHaveBeenCalled()
  stopSession()
  expect(mocks.stop).toHaveBeenCalledTimes(1)
})

it('requests a session image through the shared transport with cancellation', async () => {
  mocks.call.mockResolvedValue({ png: 'png-bytes' })
  const params = { session: 'training', path: 'attempts/1/failure/image.png' }
  const options = { signal: new AbortController().signal }
  await expect(sessionImage(params, options)).resolves.toEqual({ png: 'png-bytes' })
  expect(mocks.call).toHaveBeenCalledWith('robotlab.session.image', params, options)
})
