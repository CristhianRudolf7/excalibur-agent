import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { act, cleanup, render } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ConversationEvent } from '@/lib/rpc/contract'
import type { subscribeEvents } from '@/lib/rpc/events'
import { LabConnection } from './LabConnection'
import { useLab } from './state'
const mocks = vi.hoisted(() => ({ navigate: vi.fn(), subscribe: vi.fn(), stop: vi.fn() }))
vi.mock('@tanstack/react-router', () => ({ useNavigate: () => mocks.navigate }))
vi.mock('@/lib/rpc/events', () => ({ subscribeEvents: mocks.subscribe }))
vi.mock('@/lib/rpc/client', () => ({ rpc: { call: vi.fn() } }))
let subscription: Parameters<typeof subscribeEvents>[0]
beforeEach(() => {
  vi.clearAllMocks()
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date('2026-11-04T12:00:00Z'))
  mocks.subscribe.mockImplementation((options: typeof subscription) => {
    subscription = options
    return mocks.stop
  })
  useLab.setState({ agentOpen: false, focusedSession: undefined })
})
afterEach(() => {
  cleanup()
  vi.useRealTimers()
})
function connect() {
  const client = new QueryClient()
  const invalidate = vi.spyOn(client, 'invalidateQueries')
  render(
    <QueryClientProvider client={client}>
      <LabConnection />
    </QueryClientProvider>,
  )
  return { client, invalidate }
}
function emit(type: string, payload: unknown, at: string) {
  act(() => {
    subscription.event({
      seq: 1,
      id: 'event-1',
      type,
      at,
      // The shared envelope's payload union does not yet include Robot Lab events.
      payload: payload as ConversationEvent['payload'],
    })
  })
}
function focus(at: string) {
  emit('robotlab.focus', { session: 'training-1', robot: 'arm', environment: 'table' }, at)
}
function expectFocused() {
  expect(mocks.navigate).toHaveBeenCalledWith({
    to: '/lab',
    search: { tab: 'trainings', session: 'training-1' },
  })
  expect(useLab.getState().agentOpen).toBe(true)
}
it.each(['2026-11-04T11:55:00Z', '2026-11-04T11:59:44.999Z', '', 'invalid'])(
  'ignores replayed stale or undated focus (%s) without opening the agent',
  (at) => {
    const { invalidate } = connect()
    focus(at)
    expect(mocks.navigate).not.toHaveBeenCalled()
    expect(useLab.getState().agentOpen).toBe(false)
    expect(useLab.getState().focusedSession).toBeUndefined()
    expect(invalidate).not.toHaveBeenCalled()
  },
)
it.each(['2026-11-04T11:59:50Z', '2026-11-04T11:59:45Z'])(
  'opens a replayed focus within the inclusive 15 second window (%s)',
  (at) => {
    connect()
    focus(at)
    expectFocused()
  },
)
it.each(['2026-11-04T12:00:01Z', ''])(
  'opens live focus from any route with the shared agent column (%s)',
  (at) => {
    const { invalidate } = connect()
    subscription.ready()
    vi.setSystemTime(new Date('2026-11-04T12:00:01Z'))
    focus(at)
    expectFocused()
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ['robotlab'] })
  },
)
it('accepts focus created after subscribing even when delivery is delayed', () => {
  connect()
  subscription.ready()
  vi.setSystemTime(new Date('2026-11-04T12:01:00Z'))
  focus('2026-11-04T12:00:01Z')
  expectFocused()
})
it('ignores stale and undated replay again after reconnecting', () => {
  connect()
  subscription.ready()
  subscription.offline()
  vi.setSystemTime(new Date('2026-11-04T12:01:00Z'))
  focus('2026-11-04T11:55:00Z')
  focus('')
  expect(mocks.navigate).not.toHaveBeenCalled()
  subscription.ready()
  focus('')
  expectFocused()
})
it('refreshes session headers, lists and skills on replayed session events', () => {
  const { client, invalidate } = connect()
  const session = { id: 'training-1', status: 'stopped' }
  emit('robotlab.session', { session }, '2026-11-04T11:55:00Z')
  expect(client.getQueryData(['robotlab', 'session', 'training-1'])).toEqual({ session })
  expect(invalidate).toHaveBeenCalledWith({ queryKey: ['robotlab', 'sessions'] })
  expect(invalidate).toHaveBeenCalledWith({ queryKey: ['robotlab', 'skills', 'training-1'] })
  expect(mocks.navigate).not.toHaveBeenCalled()
})
