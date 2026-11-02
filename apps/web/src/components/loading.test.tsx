import { act, renderHook } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'
import { useLoading } from './shared'
afterEach(() => vi.useRealTimers())
test('short requests stay hidden and revealed skeletons last at least 300ms', () => {
  vi.useFakeTimers()
  const { result, rerender } = renderHook(({ pending }) => useLoading(pending), {
    initialProps: { pending: true },
  })
  act(() => vi.advanceTimersByTime(100))
  rerender({ pending: false })
  act(() => vi.advanceTimersByTime(400))
  expect(result.current.visible).toBe(false)
  rerender({ pending: true })
  act(() => vi.advanceTimersByTime(150))
  expect(result.current.visible).toBe(true)
  rerender({ pending: false })
  act(() => vi.advanceTimersByTime(299))
  expect(result.current.busy).toBe(true)
  act(() => vi.advanceTimersByTime(1))
  expect(result.current.busy).toBe(false)
})
