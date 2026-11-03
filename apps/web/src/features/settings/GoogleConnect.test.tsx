import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { type ReactNode } from 'react'
import '@/lib/i18n'
import i18n from '@/lib/i18n'
import { rpc } from '@/lib/rpc/client'
import type { GoogleStatus } from '@/lib/rpc/contract'
import { GoogleConnect } from './GoogleConnect'
import { useGoogleNotice } from './google-flow'
let client: QueryClient
beforeEach(async () => {
  await i18n.changeLanguage('en')
  HTMLDialogElement.prototype.showModal = vi.fn(function (this: HTMLDialogElement) {
    this.setAttribute('open', '')
  })
  HTMLDialogElement.prototype.close = vi.fn()
  useGoogleNotice.getState().setNotice(null)
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
})
afterEach(() => {
  cleanup()
  client.clear()
  vi.restoreAllMocks()
})
function setup(status: GoogleStatus) {
  const call = vi.spyOn(rpc, 'call').mockImplementation(async (method) => {
    if (method === 'google.disconnect') status = { connected: false, configured: true }
    return status
  })
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  )
  render(<GoogleConnect />, { wrapper })
  return call
}
it('explains the missing client and disables connection', async () => {
  setup({ connected: false, configured: false })
  expect(await screen.findByText('Google is not configured')).toBeVisible()
  expect(screen.getByText('~/.config/excalibur/credentials/google-oauth.json')).toBeVisible()
  expect(screen.getByRole('button', { name: 'Connect Google' })).toBeDisabled()
})
it('shows a disconnected account and offers connection', async () => {
  setup({ connected: false, configured: true })
  expect(await screen.findByText('No account connected')).toBeVisible()
  expect(screen.getByRole('button', { name: 'Connect Google' })).toBeEnabled()
  expect(screen.queryByRole('button', { name: 'Disconnect' })).not.toBeInTheDocument()
})
it('shows connected email, permission chips, expiry and fallback warning', async () => {
  setup({
    connected: true,
    configured: true,
    email: 'person@example.test',
    scopes: [
      'https://mail.google.com/',
      'https://www.googleapis.com/auth/drive',
      'https://www.googleapis.com/auth/documents',
      'openid',
      'email',
    ],
    expires_at: new Date(Date.now() + 24 * 3_600_000).toISOString(),
    warning: 'file_storage',
  })
  expect(await screen.findByText('Connected as person@example.test')).toBeVisible()
  const chips = within(screen.getByRole('list', { name: 'Granted permissions' }))
  for (const name of ['Gmail', 'Drive', 'Docs', 'Identity and email'])
    expect(chips.getByText(name)).toBeVisible()
  expect(screen.getByText(/Authorization expires in 24 hours/)).toBeVisible()
  expect(screen.getByText(/Less than two days remain/)).toBeVisible()
  expect(screen.getByText(/Tokens are stored in a private local file/)).toBeVisible()
  expect(screen.getByRole('button', { name: 'Reconnect' })).toBeEnabled()
})
it('requires confirmation before disconnect and refreshes status', async () => {
  const call = setup({ connected: true, configured: true, email: 'person@example.test' })
  fireEvent.click(await screen.findByRole('button', { name: 'Disconnect' }))
  expect(call).not.toHaveBeenCalledWith('google.disconnect', {})
  fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Cancel' }))
  expect(call).not.toHaveBeenCalledWith('google.disconnect', {})
  fireEvent.click(screen.getByRole('button', { name: 'Disconnect' }))
  fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Disconnect' }))
  expect(await screen.findByText('No account connected')).toBeVisible()
  expect(call).toHaveBeenCalledWith('google.disconnect', {})
})
it('names missing permissions and offers reconnect after invalid grant', async () => {
  setup({
    connected: false,
    configured: true,
    reason: 'invalid_grant',
    missing_scopes: ['https://mail.google.com/', 'https://www.googleapis.com/auth/documents'],
  })
  expect(await screen.findByText(/Missing permissions: Gmail, Docs/)).toBeVisible()
  expect(screen.getByText(/Authorization expired or was revoked/)).toBeVisible()
  expect(screen.getByRole('button', { name: 'Reconnect' })).toBeEnabled()
})
it('shows safe errors when connection start fails', async () => {
  const call = setup({ connected: false, configured: true })
  await screen.findByText('No account connected')
  call.mockRejectedValueOnce(new Error('private-token-detail'))
  fireEvent.click(screen.getByRole('button', { name: 'Connect Google' }))
  expect(await screen.findByRole('alert')).toHaveTextContent(
    'Could not update the Google connection',
  )
  expect(screen.queryByText('private-token-detail')).not.toBeInTheDocument()
})
it('shows and dismisses callback success feedback', async () => {
  useGoogleNotice.getState().setNotice('success')
  setup({ connected: true, configured: true })
  expect(await screen.findByText('Your Google account is connected.')).toBeVisible()
  fireEvent.click(screen.getByRole('button', { name: 'Close' }))
  expect(screen.queryByText('Your Google account is connected.')).not.toBeInTheDocument()
})
