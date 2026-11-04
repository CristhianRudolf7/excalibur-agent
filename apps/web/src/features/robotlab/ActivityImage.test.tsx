import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { act, cleanup, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import i18n from '@/lib/i18n'
import { ActivityImage } from './ActivityImage'

const mocks = vi.hoisted(() => ({ image: vi.fn(), disconnect: vi.fn(), observe: vi.fn() }))
vi.mock('./api', () => ({ sessionImage: mocks.image }))
let intersect: (visible: boolean) => void
let client: QueryClient
beforeEach(async () => {
  vi.clearAllMocks()
  await i18n.changeLanguage('en')
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  mocks.image.mockResolvedValue({ png: 'observation' })
  vi.stubGlobal(
    'IntersectionObserver',
    class {
      constructor(callback: IntersectionObserverCallback) {
        intersect = (visible) =>
          callback(
            [{ isIntersecting: visible } as IntersectionObserverEntry],
            this as unknown as IntersectionObserver,
          )
      }
      observe = mocks.observe
      disconnect = mocks.disconnect
    },
  )
})
afterEach(() => {
  cleanup()
  client.clear()
  vi.unstubAllGlobals()
})
function mount(session = 'training', path = 'attempts/1/looks/1.png') {
  return render(
    <QueryClientProvider client={client}>
      <ActivityImage session={session} path={path} />
    </QueryClientProvider>,
  )
}
it('loads on intersection and caches each path within its session across remounts', async () => {
  let view = mount()
  expect(mocks.observe).toHaveBeenCalledOnce()
  expect(mocks.image).not.toHaveBeenCalled()
  act(() => intersect(false))
  expect(mocks.image).not.toHaveBeenCalled()
  act(() => intersect(true))
  expect(await screen.findByRole('img', { name: 'Observation' })).toHaveAttribute(
    'src',
    'data:image/png;base64,observation',
  )
  expect(screen.getByRole('img')).toHaveAttribute('loading', 'lazy')
  expect(mocks.image).toHaveBeenCalledExactlyOnceWith(
    { session: 'training', path: 'attempts/1/looks/1.png' },
    { signal: expect.any(AbortSignal) },
  )
  expect(mocks.disconnect).toHaveBeenCalled()
  view.unmount()
  view = mount()
  act(() => intersect(true))
  await screen.findByRole('img')
  expect(mocks.image).toHaveBeenCalledTimes(1)
  view.unmount()
  view = mount('training', 'attempts/1/failure/image.png')
  act(() => intersect(true))
  await screen.findByRole('img')
  expect(mocks.image).toHaveBeenCalledTimes(2)
  view.unmount()
  mount('other-training', 'attempts/1/looks/1.png')
  act(() => intersect(true))
  await screen.findByRole('img')
  expect(mocks.image).toHaveBeenCalledTimes(3)
})
it('reports an image RPC failure without rendering a broken image or retrying', async () => {
  mocks.image.mockRejectedValue(new Error('Image unavailable'))
  mount()
  act(() => intersect(true))
  expect(await screen.findByRole('alert')).toHaveTextContent('Image unavailable')
  expect(screen.queryByRole('img')).not.toBeInTheDocument()
  expect(mocks.image).toHaveBeenCalledOnce()
})
it('cancels an outstanding image request when the thumbnail is removed', async () => {
  mocks.image.mockImplementation(() => new Promise(() => {}))
  const view = mount()
  act(() => intersect(true))
  await waitFor(() => expect(mocks.image).toHaveBeenCalledOnce())
  const signal: AbortSignal = mocks.image.mock.calls[0]![1].signal
  view.unmount()
  expect(signal.aborted).toBe(true)
})
