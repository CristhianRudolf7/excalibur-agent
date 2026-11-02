import { act, renderHook } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { useLoadingState, loadWithSkeleton } from './loading'

afterEach(() => vi.useRealTimers())
it('skips quick requests and retains a shown skeleton for 300ms', () => {
  vi.useFakeTimers()
  const { result, rerender } = renderHook(({ pending }) => useLoadingState(pending), {
    initialProps: { pending: true },
  })
  act(() => vi.advanceTimersByTime(100))
  rerender({ pending: false })
  expect(result.current).toEqual({ visible: false, holding: false })
  rerender({ pending: true })
  act(() => vi.advanceTimersByTime(150))
  expect(result.current.visible).toBe(true)
  act(() => vi.advanceTimersByTime(40))
  rerender({ pending: false })
  act(() => vi.advanceTimersByTime(259))
  expect(result.current).toEqual({ visible: true, holding: true })
  act(() => vi.advanceTimersByTime(1))
  expect(result.current).toEqual({ visible: false, holding: false })
})
it('continues displaying a skeleton when a new request interrupts its hold', () => {
  vi.useFakeTimers()
  const { result, rerender } = renderHook(({ pending }) => useLoadingState(pending), {
    initialProps: { pending: true },
  })
  act(() => vi.advanceTimersByTime(150))
  rerender({ pending: false })
  act(() => vi.advanceTimersByTime(100))
  rerender({ pending: true })
  act(() => vi.advanceTimersByTime(500))
  expect(result.current.visible).toBe(true)
  rerender({ pending: false })
  act(() => vi.advanceTimersByTime(0))
  expect(result.current.holding).toBe(false)
})
it('retains a lazy fallback only when the import exceeded its display delay', async () => {
  vi.useFakeTimers()
  const ready = vi.fn()
  const loading = loadWithSkeleton(
    () => new Promise<string>((resolve) => setTimeout(() => resolve('ready'), 200)),
  ).then(ready)
  await vi.advanceTimersByTimeAsync(449)
  expect(ready).not.toHaveBeenCalled()
  await vi.advanceTimersByTimeAsync(1)
  await loading
  expect(ready).toHaveBeenCalledWith('ready')
})
