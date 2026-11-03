import { StrictMode, type ReactNode } from 'react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import '@/lib/i18n'
import i18n from '@/lib/i18n'
import { rpc } from '@/lib/rpc/client'
import { GoogleCallback } from './GoogleCallback'
import { googleStateKey, useGoogleNotice } from './google-flow'
const navigate = vi.hoisted(() => vi.fn())
vi.mock('@tanstack/react-router', () => ({ useNavigate: () => navigate }))
let client: QueryClient
beforeEach(async () => {
  await i18n.changeLanguage('en')
  navigate.mockReset()
  useGoogleNotice.getState().setNotice(null)
  sessionStorage.clear()
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
})
afterEach(() => {
  cleanup()
  client.clear()
  vi.restoreAllMocks()
  window.history.replaceState(null, '', '/')
})
function setup(query: string) {
  window.history.replaceState(null, '', `/oauth/google/callback${query}`)
  const wrapper = ({ children }: { children: ReactNode }) => (
    <StrictMode>
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    </StrictMode>
  )
  render(<GoogleCallback />, { wrapper })
}
it('exchanges once in StrictMode, clears parameters and returns to Connections', async () => {
  const call = vi
    .spyOn(rpc, 'call')
    .mockResolvedValue({ connected: true, configured: true, email: 'person@example.test' })
  sessionStorage.setItem(googleStateKey, 'fake-state')
  setup('?code=fake-code&state=fake-state')
  expect(screen.getByRole('status')).toHaveTextContent('Connecting your Google account…')
  await waitFor(() =>
    expect(navigate).toHaveBeenCalledWith({
      to: '/settings',
      search: { section: 'connections' },
      replace: true,
    }),
  )
  expect(call).toHaveBeenCalledExactlyOnceWith('google.connect.complete', {
    code: 'fake-code',
    state: 'fake-state',
  })
  expect(window.location.search).toBe('')
  expect(sessionStorage.getItem(googleStateKey)).toBeNull()
  expect(useGoogleNotice.getState().notice).toBe('success')
})
it.each([
  '?error=access_denied&state=fake-state',
  '?code=fake-code',
  '?state=fake-state',
  '?error=server_error',
])('handles incomplete or denied callback %s', async (query) => {
  const call = vi.spyOn(rpc, 'call')
  setup(query)
  await waitFor(() => expect(navigate).toHaveBeenCalled())
  expect(call).not.toHaveBeenCalled()
  expect(useGoogleNotice.getState().notice).toBe(
    query.includes('access_denied') ? 'denied' : 'error',
  )
})
it('rejects browser state mismatch', async () => {
  const call = vi.spyOn(rpc, 'call')
  sessionStorage.setItem(googleStateKey, 'expected')
  setup('?code=fake-code&state=wrong')
  await waitFor(() => expect(navigate).toHaveBeenCalled())
  expect(call).not.toHaveBeenCalled()
  expect(useGoogleNotice.getState().notice).toBe('error')
})
it('returns safe feedback after expired state or a network error', async () => {
  vi.spyOn(rpc, 'call').mockRejectedValue(new Error('private-exchange-detail'))
  setup('?code=fake-code&state=fake-state')
  await waitFor(() => expect(navigate).toHaveBeenCalled())
  expect(useGoogleNotice.getState().notice).toBe('error')
  expect(screen.queryByText('private-exchange-detail')).not.toBeInTheDocument()
})
it('reports partial consent for reconnection', async () => {
  vi.spyOn(rpc, 'call').mockResolvedValue({
    connected: true,
    configured: true,
    missing_scopes: ['https://mail.google.com/'],
  })
  setup('?code=fake-code&state=fake-state')
  await waitFor(() => expect(navigate).toHaveBeenCalled())
  expect(useGoogleNotice.getState().notice).toBe('missing')
})
