import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import i18n from '@/lib/i18n'
import { rpc } from '@/lib/rpc/client'
import type { CodexStatus } from '@/lib/rpc/contract'
import { CodexConnect } from './CodexConnect'

const disconnected: CodexStatus = {
  connected: false,
  source: '',
  account_email: '',
  plan_type: '',
}
const connected: CodexStatus = {
  connected: true,
  source: 'excalibur',
  account_email: 'person@example.test',
  plan_type: 'plus',
}
const authURL = 'https://auth.example.test/oauth/authorize?state=test-state'
let client: QueryClient
beforeEach(async () => {
  await i18n.changeLanguage('en')
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
})
afterEach(() => {
  cleanup()
  client.clear()
  vi.useRealTimers()
  vi.restoreAllMocks()
})
function setup(initial: CodexStatus = disconnected) {
  let status = initial
  const call = vi.spyOn(rpc, 'call').mockImplementation(async (method) => {
    if (method === 'codex.login.start') return { auth_url: authURL }
    if (method === 'codex.login.complete') status = connected
    if (method === 'codex.logout') status = disconnected
    return status
  })
  render(
    <QueryClientProvider client={client}>
      <CodexConnect />
    </QueryClientProvider>,
  )
  return { call, setStatus: (next: CodexStatus) => (status = next) }
}
it.each([
  ['en', 'ChatGPT / Codex account', 'Connect', 'Usage counts against your ChatGPT plan limits.'],
  [
    'es',
    'Cuenta de ChatGPT / Codex',
    'Conectar',
    'El uso cuenta contra los límites de tu plan de ChatGPT.',
  ],
])('renders the account card in %s', async (language, title, button, limits) => {
  await i18n.changeLanguage(language)
  setup()
  expect(await screen.findByRole('region', { name: title })).toBeVisible()
  expect(screen.getByRole('button', { name: button })).toBeEnabled()
  expect(screen.getByText(limits)).toBeVisible()
})
it.each([
  ['excalibur', 'Excalibur'],
  ['codex-cli', 'Codex CLI'],
  ['env', 'environment-configured file'],
] as const)('shows connected metadata from %s and disconnects', async (source, label) => {
  const { call } = setup({ ...connected, source })
  expect(await screen.findByText('Connected as person@example.test')).toBeVisible()
  expect(screen.getByText('Plan: plus')).toBeVisible()
  expect(screen.getByText(`Source: ${label}`)).toBeVisible()
  if (source !== 'excalibur') expect(screen.getByText(/sign out at its source/)).toBeVisible()
  fireEvent.click(screen.getByRole('button', { name: 'Disconnect' }))
  expect(await screen.findByText('No account connected')).toBeVisible()
  expect(call).toHaveBeenCalledWith('codex.logout', {})
})
it.each(['http://localhost:1455/auth/callback?code=test-code&state=test-state', 'test-code'])(
  'completes remote sign-in using the pasted URL or code',
  async (paste) => {
    const { call } = setup()
    await screen.findByText('No account connected')
    fireEvent.click(screen.getByRole('button', { name: 'Connect' }))
    expect(await screen.findByRole('link', { name: 'Open Sign in with ChatGPT' })).toHaveAttribute(
      'href',
      authURL,
    )
    expect(screen.getByText(/even if the page cannot load/)).toBeVisible()
    expect(screen.getByRole('button', { name: 'Complete connection' })).toBeDisabled()
    fireEvent.change(screen.getByLabelText('Final redirect URL or code'), {
      target: { value: paste },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Complete connection' }))
    expect(await screen.findByText('Connected as person@example.test')).toBeVisible()
    expect(call).toHaveBeenCalledWith(
      'codex.login.complete',
      { code: paste, state: 'test-state' },
      { timeoutMs: 30_000 },
    )
    expect(screen.queryByLabelText('Final redirect URL or code')).not.toBeInTheDocument()
    expect(localStorage.getItem('codex-auth')).toBeNull()
  },
)
it('observes loopback completion through refreshed status and clears pasted input', async () => {
  const { setStatus } = setup()
  fireEvent.click(await screen.findByRole('button', { name: 'Connect' }))
  fireEvent.change(await screen.findByLabelText('Final redirect URL or code'), {
    target: { value: 'temporary-code' },
  })
  setStatus(connected)
  await act(() => client.invalidateQueries({ queryKey: ['codex', 'status'] }))
  expect(await screen.findByText('Connected as person@example.test')).toBeVisible()
  expect(screen.queryByLabelText('Final redirect URL or code')).not.toBeInTheDocument()
})
it.each(['start', 'complete', 'logout', 'status'])('redacts %s failures', async (stage) => {
  const { call } = setup(stage === 'logout' ? connected : disconnected)
  await screen.findByText(
    stage === 'logout' ? 'Connected as person@example.test' : 'No account connected',
  )
  if (stage === 'complete') {
    fireEvent.click(screen.getByRole('button', { name: 'Connect' }))
    fireEvent.change(await screen.findByLabelText('Final redirect URL or code'), {
      target: { value: 'private-auth-code' },
    })
  }
  call.mockRejectedValueOnce(new Error('private-token-detail'))
  if (stage === 'status') {
    await act(() => client.invalidateQueries({ queryKey: ['codex', 'status'] }))
  } else {
    fireEvent.click(
      screen.getByRole('button', {
        name:
          stage === 'complete'
            ? 'Complete connection'
            : stage === 'logout'
              ? 'Disconnect'
              : 'Connect',
      }),
    )
  }
  expect(await screen.findByRole('alert')).toHaveTextContent('Could not update the connection')
  expect(screen.queryByText(/private-token-detail|private-auth-code/)).not.toBeInTheDocument()
})
it('rejects unsafe authorization URLs without offering a link', async () => {
  const { call } = setup()
  await screen.findByText('No account connected')
  call.mockResolvedValueOnce({ auth_url: 'javascript:alert(1)' })
  fireEvent.click(screen.getByRole('button', { name: 'Connect' }))
  expect(await screen.findByRole('alert')).toHaveTextContent('Could not update the connection')
  expect(screen.queryByRole('link')).not.toBeInTheDocument()
})
it('cancels pending sign-in through logout', async () => {
  const { call } = setup()
  fireEvent.click(await screen.findByRole('button', { name: 'Connect' }))
  fireEvent.click(await screen.findByRole('button', { name: 'Disconnect' }))
  await waitFor(() =>
    expect(screen.queryByLabelText('Final redirect URL or code')).not.toBeInTheDocument(),
  )
  expect(call).toHaveBeenCalledWith('codex.logout', {})
})
it('expires the paste form and removes sensitive input from the card', async () => {
  setup()
  fireEvent.click(await screen.findByRole('button', { name: 'Connect' }))
  const input = await screen.findByLabelText('Final redirect URL or code')
  fireEvent.change(input, { target: { value: 'temporary-code' } })
  vi.useFakeTimers()
  // Restart the flow so its timer is installed on the fake clock.
  fireEvent.click(screen.getByRole('button', { name: 'Connect' }))
  await act(async () => {})
  await act(() => vi.advanceTimersByTimeAsync(10 * 60_000))
  expect(screen.getByRole('alert')).toHaveTextContent('The link expired')
  expect(screen.queryByLabelText('Final redirect URL or code')).not.toBeInTheDocument()
})
