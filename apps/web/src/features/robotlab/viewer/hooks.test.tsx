import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { act, cleanup, renderHook as baseRenderHook, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import type { Frame } from './types'
import { RpcError } from '@/lib/rpc/client'
const mocks = vi.hoisted(() => ({ call: vi.fn(), subscribe: vi.fn(), stop: vi.fn() }))
vi.mock('./rpc', () => ({ viewerCall: mocks.call, subscribeFrames: mocks.subscribe }))
import { useLatestRecordedFrame, useLiveFrame, useReplay, useScene } from './hooks'
import { frameFixture, sceneFixture } from './fixtures.test-support'
let queryClient: QueryClient
function wrapper({ children }: { children: ReactNode }) {
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
}
const renderHook: typeof baseRenderHook = (callback, options) =>
  baseRenderHook(callback, { ...options, wrapper })
let receive: (frame: Frame) => void
let now = 0
let next = 0
const callbacks = new Map<number, FrameRequestCallback>()
beforeEach(() => {
  queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  mocks.call.mockReset()
  mocks.stop.mockReset()
  mocks.subscribe.mockImplementation((callback) => {
    receive = callback
    return mocks.stop
  })
  callbacks.clear()
  now = 0
  vi.spyOn(performance, 'now').mockImplementation(() => now)
  vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => {
    callbacks.set(++next, callback)
    return next
  })
  vi.stubGlobal('cancelAnimationFrame', (id: number) => callbacks.delete(id))
})
afterEach(() => {
  cleanup()
  queryClient.clear()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  vi.useRealTimers()
})
function tick(ms: number) {
  act(() => {
    now += ms
    const pending = [...callbacks.values()]
    callbacks.clear()
    pending.forEach((callback) => callback(now))
  })
}
it('filters live sessions, coalesces per animation frame, ignores stale frames and resets on session changes', () => {
  const hook = renderHook(({ session }) => useLiveFrame(session), {
    initialProps: { session: 'session' },
  })
  act(() => {
    receive(frameFixture(1, { session: 'other' }))
    receive(frameFixture(1))
    receive(frameFixture(3))
    receive(frameFixture(2))
  })
  expect(hook.result.current).toBeNull()
  expect(callbacks.size).toBe(1)
  tick(16)
  expect(hook.result.current?.seq).toBe(3)
  act(() => receive(frameFixture(0, { attempt: 2 })))
  tick(16)
  expect(hook.result.current?.attempt).toBe(2)
  act(() => receive(frameFixture(4)))
  expect(callbacks.size).toBe(0)
  act(() => receive(frameFixture(1, { attempt: 2 })))
  hook.rerender({ session: 'other' })
  expect(hook.result.current).toBeNull()
  expect(mocks.stop).toHaveBeenCalledOnce()
  expect(callbacks.size).toBe(0)
  hook.unmount()
  expect(mocks.stop).toHaveBeenCalledTimes(2)
})
it('does not subscribe without a session', () => {
  mocks.subscribe.mockClear()
  const { result } = renderHook(() => useLiveFrame(undefined))
  expect(result.current).toBeNull()
  expect(mocks.subscribe).not.toHaveBeenCalled()
})
it('loads scene variants and aborts stale scene requests', async () => {
  let resolveOld: (value: { scene: typeof sceneFixture }) => void = () => {}
  mocks.call.mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        resolveOld = resolve
      }),
  )
  const hook = renderHook(
    ({ kind, id }: { kind: 'session' | 'env'; id: string | undefined }) => useScene(kind, id),
    { initialProps: { kind: 'session', id: 'old' } },
  )
  const oldSignal = mocks.call.mock.calls[0]![2].signal as AbortSignal
  mocks.call.mockResolvedValueOnce({ scene: sceneFixture })
  hook.rerender({ kind: 'env', id: 'room' })
  expect(oldSignal.aborted).toBe(true)
  await waitFor(() => expect(hook.result.current.scene).toEqual(sceneFixture))
  await act(async () => resolveOld({ scene: { ...sceneFixture, environment: 'old' } }))
  expect(hook.result.current.scene?.environment).toBe('room')
  expect(mocks.call).toHaveBeenLastCalledWith(
    'robotlab.env.scene',
    { id: 'room' },
    expect.anything(),
  )
  hook.rerender({ kind: 'env', id: undefined })
  expect(hook.result.current).toEqual({ scene: null, loading: false })
})
it('keeps live scenes loading during exponential retries and recovers without remounting', async () => {
  vi.useFakeTimers()
  mocks.call
    .mockRejectedValueOnce(new RpcError(-32005, 'an action is already running'))
    .mockRejectedValueOnce(new RpcError(-32020, 'runtime restarting'))
    .mockResolvedValueOnce({ scene: sceneFixture })
  const hook = renderHook(() => useScene('session', 'session', true))
  await act(() => vi.advanceTimersByTimeAsync(999))
  expect(mocks.call).toHaveBeenCalledTimes(1)
  expect(hook.result.current).toEqual({ scene: null, loading: true })
  await act(() => vi.advanceTimersByTimeAsync(1))
  expect(mocks.call).toHaveBeenCalledTimes(2)
  expect(hook.result.current.error).toBeUndefined()
  await act(() => vi.advanceTimersByTimeAsync(2001))
  expect(mocks.call).toHaveBeenCalledTimes(3)
  expect(hook.result.current.scene).toEqual(sceneFixture)
  expect(hook.result.current.loading).toBe(false)
})
it('ends retries when a session stops and cancels retry timers on unmount', async () => {
  vi.useFakeTimers()
  mocks.call.mockRejectedValue(new RpcError(-32005, 'an action is already running'))
  const hook = renderHook(({ live }) => useScene('session', 'session', live), {
    initialProps: { live: true },
  })
  await act(() => vi.advanceTimersByTimeAsync(0))
  hook.rerender({ live: false })
  await act(() => vi.advanceTimersByTimeAsync(1001))
  expect(hook.result.current.error).toBeTruthy()
  const calls = mocks.call.mock.calls.length
  await act(() => vi.advanceTimersByTimeAsync(60_000))
  expect(mocks.call).toHaveBeenCalledTimes(calls)
  hook.unmount()
  const other = renderHook(() => useScene('session', 'other', true))
  await act(() => vi.advanceTimersByTimeAsync(0))
  const signal = mocks.call.mock.calls.at(-1)![2].signal as AbortSignal
  other.unmount()
  expect(signal.aborted).toBe(true)
  const before = mocks.call.mock.calls.length
  await act(() => vi.advanceTimersByTimeAsync(60_000))
  expect(mocks.call).toHaveBeenCalledTimes(before)
})
it('does not retry permanent scene errors even while live', async () => {
  mocks.call.mockRejectedValue(new RpcError(-32001, 'session not found'))
  const hook = renderHook(() => useScene('session', 'missing', true))
  await waitFor(() => expect(hook.result.current.error).toBeTruthy())
  expect(mocks.call).toHaveBeenCalledOnce()
})
it('plays, pauses, seeks, changes speed and stops at the recording end', async () => {
  mocks.call.mockResolvedValue({
    frames: Array.from({ length: 21 }, (_, i) => frameFixture(i)),
    total: 21,
  })
  const hook = renderHook(() => useReplay('session', 1))
  expect(hook.result.current.loading).toBe(true)
  await waitFor(() => expect(hook.result.current.loading).toBe(false))
  expect(hook.result.current.duration).toBe(1)
  expect(hook.result.current.frame?.seq).toBe(0)
  act(() => hook.result.current.play())
  tick(100)
  expect(hook.result.current.frame?.seq).toBe(2)
  act(() => hook.result.current.pause())
  tick(100)
  expect(hook.result.current.t).toBeCloseTo(0.1)
  act(() => {
    hook.result.current.seek(0.5)
    hook.result.current.setSpeed(2)
    hook.result.current.play()
  })
  tick(100)
  expect(hook.result.current.frame?.seq).toBe(14)
  tick(200)
  expect(hook.result.current.frame?.seq).toBe(20)
  expect(hook.result.current.playing).toBe(false)
  expect(callbacks.size).toBe(0)
  act(() => hook.result.current.play())
  expect(hook.result.current.t).toBe(0)
  act(() => hook.result.current.seek(100))
  expect(hook.result.current.playing).toBe(false)
  act(() => {
    hook.result.current.seek(-100)
    hook.result.current.setSpeed(NaN)
  })
  expect(hook.result.current.t).toBe(0)
  expect(hook.result.current.speed).toBe(2)
})
it('cancels playback and pending loads when the attempt changes', async () => {
  mocks.call.mockResolvedValueOnce({ frames: [frameFixture(0), frameFixture(20)], total: 2 })
  const hook = renderHook(({ attempt }) => useReplay('session', attempt), {
    initialProps: { attempt: 1 },
  })
  await waitFor(() => expect(hook.result.current.loading).toBe(false))
  act(() => hook.result.current.play())
  let resolveOld: (value: { frames: Frame[]; total: number }) => void = () => {}
  mocks.call.mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        resolveOld = resolve
      }),
  )
  hook.rerender({ attempt: 2 })
  expect(hook.result.current.frame).toBeNull()
  expect(hook.result.current.playing).toBe(false)
  expect(callbacks.size).toBe(0)
  const signal = mocks.call.mock.calls.at(-1)![2].signal as AbortSignal
  mocks.call.mockResolvedValueOnce({ frames: [frameFixture(1, { attempt: 3 })], total: 1 })
  hook.rerender({ attempt: 3 })
  expect(signal.aborted).toBe(true)
  await waitFor(() => expect(hook.result.current.frame?.attempt).toBe(3))
  await act(async () => resolveOld({ frames: [frameFixture(1, { attempt: 2 })], total: 1 }))
  expect(hook.result.current.frame?.attempt).toBe(3)
})
it('reports load errors and handles empty recordings without playback', async () => {
  mocks.call.mockRejectedValueOnce(new Error('offline'))
  const scene = renderHook(() => useScene('env', 'room'))
  await waitFor(() => expect(scene.result.current.error).toBeTruthy())
  mocks.call.mockRejectedValueOnce(new Error('offline'))
  const hook = renderHook(({ attempt }) => useReplay('session', attempt), {
    initialProps: { attempt: 1 },
  })
  await waitFor(() => expect(hook.result.current.error).toBeTruthy())
  mocks.call.mockResolvedValueOnce({ frames: [], total: 0 })
  hook.rerender({ attempt: 2 })
  await waitFor(() => expect(hook.result.current.loading).toBe(false))
  act(() => hook.result.current.play())
  expect(hook.result.current.playing).toBe(false)
  expect(hook.result.current.frame).toBeNull()
  expect(hook.result.current.error).toBeUndefined()
})

it('clears old live frames and rejects late frames when the same session gets a new attempt', () => {
  const hook = renderHook(({ attempt }) => useLiveFrame('session', attempt), {
    initialProps: { attempt: 1 },
  })
  act(() => receive(frameFixture(20)))
  tick(16)
  expect(hook.result.current?.seq).toBe(20)
  hook.rerender({ attempt: 2 })
  expect(hook.result.current).toBeNull()
  act(() => receive(frameFixture(21)))
  tick(16)
  expect(hook.result.current).toBeNull()
  act(() => receive(frameFixture(1, { attempt: 2 })))
  tick(16)
  expect(hook.result.current?.attempt).toBe(2)
  expect(hook.result.current?.seq).toBe(1)
})

it('fetches only the head and final record to initialize the last known pose', async () => {
  mocks.call
    .mockResolvedValueOnce({ frames: [frameFixture(0)], total: 50 })
    .mockResolvedValueOnce({ frames: [frameFixture(49)], total: 50 })
  const hook = renderHook(() => useLatestRecordedFrame('session', 1))
  expect(hook.result.current).toBeNull()
  await waitFor(() => expect(hook.result.current?.seq).toBe(49))
  expect(mocks.call.mock.calls.map(([, params]) => params)).toEqual([
    { session: 'session', attempt: 1, from: 0, count: 1 },
    { session: 'session', attempt: 1, from: 49, count: 1 },
  ])
})

it('falls back through empty attempts to the previous attempt with a recorded pose', async () => {
  mocks.call
    .mockResolvedValueOnce({ frames: [], total: 0 })
    .mockResolvedValueOnce({ frames: [], total: 0 })
    .mockResolvedValueOnce({ frames: [frameFixture(0)], total: 10 })
    .mockResolvedValueOnce({ frames: [frameFixture(9)], total: 10 })
  const hook = renderHook(() => useLatestRecordedFrame('session', 3))
  await waitFor(() => expect(hook.result.current?.seq).toBe(9))
  expect(hook.result.current?.attempt).toBe(1)
  expect(mocks.call.mock.calls.map(([, params]) => params.attempt)).toEqual([3, 2, 1, 1])
})

it('keeps the pose unknown when no attempt has frames and does not query attempt zero', async () => {
  mocks.call.mockResolvedValue({ frames: [], total: 0 })
  const hook = renderHook(() => useLatestRecordedFrame('session', 2))
  await waitFor(() => expect(mocks.call).toHaveBeenCalledTimes(2))
  expect(hook.result.current).toBeNull()
  expect(mocks.call.mock.calls.map(([, params]) => params.attempt)).toEqual([2, 1])
})

it('cancels initial pose loads on session changes and ignores their late responses', async () => {
  let finish: (value: { frames: Frame[]; total: number }) => void = () => {}
  mocks.call.mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        finish = resolve
      }),
  )
  const hook = renderHook(({ session }) => useLatestRecordedFrame(session, 1), {
    initialProps: { session: 'session' },
  })
  const signal = mocks.call.mock.calls[0]![2].signal as AbortSignal
  mocks.call.mockResolvedValueOnce({ frames: [frameFixture(1, { session: 'next' })], total: 1 })
  hook.rerender({ session: 'next' })
  expect(signal.aborted).toBe(true)
  await waitFor(() => expect(hook.result.current?.session).toBe('next'))
  await act(async () => finish({ frames: [frameFixture(20)], total: 1 }))
  expect(hook.result.current?.session).toBe('next')
})

it('can open a stopped replay at its last pose and restart playback from the beginning', async () => {
  mocks.call.mockResolvedValue({ frames: [frameFixture(0), frameFixture(20)], total: 2 })
  const hook = renderHook(() => useReplay('session', 1, true))
  await waitFor(() => expect(hook.result.current.loading).toBe(false))
  expect(hook.result.current.frame?.seq).toBe(20)
  expect(hook.result.current.t).toBe(1)
  act(() => hook.result.current.play())
  expect(hook.result.current.t).toBe(0)
  tick(100)
  expect(hook.result.current.t).toBeCloseTo(0.1)
  act(() => hook.result.current.seek(0.5))
  expect(hook.result.current.t).toBe(0.5)
})
