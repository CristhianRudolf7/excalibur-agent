import { afterEach, expect, it, vi } from 'vitest'
import { act, cleanup, renderHook, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider, useQuery } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import type { ConversationEvent, Reminder } from '@/lib/rpc/contract'
import { subscribeEvents } from '@/lib/rpc/events'
import { useWorkEvents } from './events'
vi.mock('@/lib/rpc/events', () => ({ subscribeEvents: vi.fn(() => vi.fn()) }))
afterEach(() => {
  cleanup()
  vi.clearAllMocks()
  vi.unstubAllEnvs()
})
it('refreshes task lists and open details, routes fired reminders and closes the socket', () => {
  const client = new QueryClient(),
    invalidate = vi.spyOn(client, 'invalidateQueries'),
    fired = vi.fn()
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  )
  const hook = renderHook(() => useWorkEvents(fired), { wrapper })
  const options = vi.mocked(subscribeEvents).mock.calls.at(-1)![0]
  expect(options.types).toEqual(['task.updated', 'reminder.updated', 'reminder.fired'])
  act(() => options.ready())
  expect(invalidate).not.toHaveBeenCalled()
  act(() => options.offline())
  act(() => options.ready())
  expect(invalidate).toHaveBeenCalledWith({ queryKey: ['work'] })
  expect(invalidate).toHaveBeenCalledOnce()
  const event = (type: string, payload: ConversationEvent['payload']): ConversationEvent => ({
    id: type,
    seq: 1,
    type,
    at: '2026-11-02T12:00:00Z',
    payload,
  })
  act(() => options.event(event('task.updated', {})))
  expect(invalidate).toHaveBeenCalledWith({ queryKey: ['work', 'tasks'] })
  expect(invalidate).toHaveBeenCalledWith({ queryKey: ['work', 'task'] })
  const reminder = { id: 'reminder-1', text: 'Review' } as Reminder
  act(() => options.event(event('reminder.updated', { reminder })))
  expect(fired).not.toHaveBeenCalled()
  act(() => options.event(event('reminder.fired', { reminder })))
  expect(invalidate).toHaveBeenCalledWith({ queryKey: ['work', 'reminders'] })
  expect(fired).toHaveBeenCalledWith(reminder)
  const close = vi.mocked(subscribeEvents).mock.results.at(-1)!.value
  hook.unmount()
  expect(close).toHaveBeenCalledOnce()
})

it('preserves an initial query failure until retry or reconnection', async () => {
  const client = new QueryClient()
  const fetchTasks = vi.fn().mockRejectedValueOnce(new Error('Unavailable')).mockResolvedValue([])
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  )
  const hook = renderHook(
    () => {
      useWorkEvents()
      return useQuery({ queryKey: ['work', 'tasks'], queryFn: fetchTasks, retry: false })
    },
    { wrapper },
  )
  await waitFor(() => expect(hook.result.current.isError).toBe(true))
  const options = vi.mocked(subscribeEvents).mock.calls.at(-1)![0]
  act(() => options.ready())
  expect(fetchTasks).toHaveBeenCalledOnce()
  expect(hook.result.current.isError).toBe(true)
  act(() => options.offline())
  act(() => options.ready())
  await waitFor(() => expect(hook.result.current.isSuccess).toBe(true))
  expect(fetchTasks).toHaveBeenCalledTimes(2)
})

it('keeps RPC views available without an event socket when the switch is off', () => {
  vi.stubEnv('EXCALIBUR_WEB_WORK_EVENTS', '0')
  const client = new QueryClient()
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  )
  renderHook(() => useWorkEvents(), { wrapper })
  expect(subscribeEvents).not.toHaveBeenCalled()
})
