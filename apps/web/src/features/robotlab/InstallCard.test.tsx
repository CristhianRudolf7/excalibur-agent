import { beforeEach, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import i18n from '@/lib/i18n'
import { InstallCard } from './InstallCard'
const mocks = vi.hoisted(() => ({
  call: vi.fn(),
  event: undefined as
    | undefined
    | ((event: { done?: boolean; error?: string; percent?: number; message: string }) => void),
}))
vi.mock('./api', () => ({
  robotlabEnabled: true,
  labCall: mocks.call,
  useRobotEvent: (_: string, callback: typeof mocks.event) => {
    mocks.event = callback
  },
}))
beforeEach(async () => {
  cleanup()
  vi.clearAllMocks()
  await i18n.changeLanguage('en')
  mocks.call.mockImplementation(async (method: string) =>
    method === 'robotlab.status' ? { installed: false, enabled: true } : { started: true },
  )
})
function mount() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={client}>
      <InstallCard />
    </QueryClientProvider>,
  )
  return client
}
it('starts installation and exposes runtime progress', async () => {
  mount()
  const button = await screen.findByRole('button', { name: 'Install robot lab' })
  await waitFor(() => expect(button).toBeEnabled())
  fireEvent.click(button)
  await waitFor(() =>
    expect(mocks.call).toHaveBeenCalledWith('robotlab.install', {}, { timeoutMs: 180_000 }),
  )
  expect(screen.getByRole('progressbar')).toBeVisible()
})
it('shows installation errors and permits retry', async () => {
  mocks.call.mockImplementation(async (method: string) => {
    if (method === 'robotlab.install') throw new Error('Install failed')
    return { installed: false, enabled: true }
  })
  mount()
  const button = await screen.findByRole('button', { name: 'Install robot lab' })
  await waitFor(() => expect(button).toBeEnabled())
  fireEvent.click(button)
  expect(await screen.findByRole('alert')).toHaveTextContent('Install failed')
  expect(screen.getByRole('button', { name: 'Install robot lab' })).toBeEnabled()
})
