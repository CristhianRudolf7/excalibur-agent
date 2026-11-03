import { expect, it, vi } from 'vitest'
import { renderHook } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { subscribeEvents } from '@/lib/rpc/events'
import { useCollectionEvents } from './api'
vi.mock('@/lib/rpc/events', () => ({ subscribeEvents: vi.fn(() => vi.fn()) }))
it('invalidates collection schemas, lists, related records and summaries after live events or reconnect', () => {
  const client = new QueryClient()
  const invalidate = vi.spyOn(client, 'invalidateQueries')
  const hook = renderHook(useCollectionEvents, {
    wrapper: ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    ),
  })
  const options = vi.mocked(subscribeEvents).mock.calls[0]![0]
  expect(options.types).toEqual(['collections.changed'])
  options.event({
    type: 'collections.changed',
    payload: { collection: 'grades', record_id: 'math', kind: 'updated' },
    seq: 1,
    id: 'event-1',
    at: '',
  })
  expect(invalidate).toHaveBeenCalledWith({ queryKey: ['mydata'] })
  invalidate.mockClear()
  options.ready()
  expect(invalidate).toHaveBeenCalledOnce()
  const stop = vi.mocked(subscribeEvents).mock.results[0]!.value
  hook.unmount()
  expect(stop).toHaveBeenCalledOnce()
})
