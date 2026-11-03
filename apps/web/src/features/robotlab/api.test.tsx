import { act, renderHook } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
const mock = vi.hoisted(() => ({ subscribe: vi.fn(), close: vi.fn(), call: vi.fn() }))
vi.mock('@/lib/rpc/events', () => ({ subscribeEvents: mock.subscribe }))
vi.mock('@/lib/rpc/client', () => ({ rpc: { call: mock.call } }))
import { labCall, onRobotEvent, useRobotEvent } from './api'
afterEach(() => vi.clearAllMocks())
it('shares one socket across event consumers and disconnects when the last consumer leaves', () => {
  mock.subscribe.mockReturnValue(mock.close)
  const frame = vi.fn(),
    metrics = vi.fn()
  const stopFrame = onRobotEvent('robotlab.frame', frame)
  const stopMetrics = onRobotEvent('robotlab.metrics', metrics)
  expect(mock.subscribe).toHaveBeenCalledTimes(1)
  const bus = mock.subscribe.mock.calls[0]![0]
  bus.event({ type: 'robotlab.metrics', payload: { experiment: 'exp', points: [] } })
  expect(metrics).toHaveBeenCalledWith({ experiment: 'exp', points: [] })
  expect(frame).not.toHaveBeenCalled()
  stopFrame()
  expect(mock.close).not.toHaveBeenCalled()
  stopMetrics()
  expect(mock.close).toHaveBeenCalledTimes(1)
})
it('event hooks use the current callback without resubscribing', () => {
  mock.subscribe.mockReturnValue(mock.close)
  const first = vi.fn(),
    second = vi.fn()
  const hook = renderHook(({ callback }) => useRobotEvent('robotlab.focus', callback), {
    initialProps: { callback: first },
  })
  hook.rerender({ callback: second })
  act(() =>
    mock.subscribe.mock.calls[0]![0].event({ type: 'robotlab.focus', payload: { view: 'scene' } }),
  )
  expect(first).not.toHaveBeenCalled()
  expect(second).toHaveBeenCalledWith({ view: 'scene' })
  hook.unmount()
  expect(mock.close).toHaveBeenCalledTimes(1)
})
it('keeps typed requests on the existing RPC transport', async () => {
  mock.call.mockResolvedValue({ mode: 'pose' })
  await labCall('robotlab.session.control', {
    session: 'ses',
    mode: 'pose',
    pose: { landmarks: [], t: 123 },
  })
  expect(mock.call).toHaveBeenCalledWith('robotlab.session.control', {
    session: 'ses',
    mode: 'pose',
    pose: { landmarks: [], t: 123 },
  })
})

it('allows a full vision loop and forwards cancellation and caller timeouts', async () => {
  await labCall('robotlab.vision.step', { session: 'ses', instruction: 'Sort the blocks' })
  expect(mock.call).toHaveBeenLastCalledWith(
    'robotlab.vision.step',
    { session: 'ses', instruction: 'Sort the blocks' },
    { timeoutMs: 180000 },
  )
  const controller = new AbortController()
  await labCall(
    'robotlab.vision.step',
    { session: 'ses', instruction: 'Sort the blocks' },
    { signal: controller.signal, timeoutMs: 30000 },
  )
  expect(mock.call).toHaveBeenLastCalledWith(
    'robotlab.vision.step',
    { session: 'ses', instruction: 'Sort the blocks' },
    { signal: controller.signal, timeoutMs: 30000 },
  )
})

it('reads mapping source through the typed transport', async () => {
  const mapping = { source: 'default', code: 'def map_pose(pose, state):\n    return {}' }
  mock.call.mockResolvedValue(mapping)
  await expect(labCall('robotlab.teleop.get', { session: 'ses' })).resolves.toEqual(mapping)
  expect(mock.call).toHaveBeenLastCalledWith('robotlab.teleop.get', { session: 'ses' })
})

it('shares teleop events and offline notifications with the lab subscription', () => {
  mock.subscribe.mockReturnValue(mock.close)
  const receive = vi.fn(),
    offline = vi.fn()
  const stop = onRobotEvent('robotlab.teleop', receive, offline)
  const bus = mock.subscribe.mock.calls[0]![0]
  expect(bus.types).toContain('robotlab.teleop')
  bus.event({ type: 'robotlab.teleop', payload: { session: 'ses', error: 'Mapping failed' } })
  expect(receive).toHaveBeenCalledWith({ session: 'ses', error: 'Mapping failed' })
  bus.offline()
  expect(offline).toHaveBeenCalledTimes(1)
  stop()
  bus.offline()
  expect(offline).toHaveBeenCalledTimes(1)
})
