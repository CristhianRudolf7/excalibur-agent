import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import i18n from '@/lib/i18n'
import { rpc, RpcError } from '@/lib/rpc/client'
import type { CredentialRequest } from '@/lib/rpc/credentials'
import { useChat } from '@/features/chat/store'
import { CredentialCard } from './CredentialCard'

const request: CredentialRequest = {
  id: 'credential-1',
  conversation_id: 'cnv_web_excalibur',
  site: 'example.org',
  purpose: 'Sign in to review your account',
  fields: ['username', 'password'],
  remember_default: true,
  status: 'pending',
  created_at: '2026-11-03T12:00:00Z',
}
beforeEach(async () => {
  await i18n.changeLanguage('en')
})
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})
function fill() {
  fireEvent.change(screen.getByLabelText('Username'), { target: { value: 'sentinel-user' } })
  fireEvent.change(screen.getByLabelText('Password', { exact: true }), {
    target: { value: 'sentinel-password' },
  })
}
it('shows the host, purpose and requested fields with safe autocomplete and visibility controls', () => {
  render(<CredentialCard request={request} onResolved={vi.fn()} />)
  expect(screen.getByText('example.org')).toBeVisible()
  expect(screen.getByText(request.purpose)).toBeVisible()
  expect(screen.getByLabelText('Remember for this site')).toBeChecked()
  expect(screen.getByLabelText('Username')).toHaveAttribute('autocomplete', 'off')
  const password = screen.getByLabelText('Password', { exact: true })
  expect(password).toHaveAttribute('type', 'password')
  expect(password).toHaveAttribute('autocomplete', 'new-password')
  fireEvent.click(screen.getByRole('button', { name: 'Show Password' }))
  expect(password).toHaveAttribute('type', 'text')
  fireEvent.click(screen.getByRole('button', { name: 'Hide Password' }))
  expect(password).toHaveAttribute('type', 'password')
})
it('submits only through the secure RPC, clears DOM and transient params, and never changes chat or storage', async () => {
  const before = JSON.stringify(useChat.getState())
  const storage = JSON.stringify(localStorage)
  let sent: unknown
  const call = vi.spyOn(rpc, 'call').mockImplementation(async (_method, params) => {
    sent = structuredClone(params)
    return { ok: true }
  })
  const resolve = vi.fn()
  const { container } = render(<CredentialCard request={request} onResolved={resolve} />)
  fill()
  const inputs = [...container.querySelectorAll('input:not([type=checkbox])')]
  fireEvent.click(screen.getByRole('button', { name: /^Send$/ }))
  await screen.findByText('Credentials sent securely')
  expect(sent).toEqual({
    request_id: request.id,
    values: { username: 'sentinel-user', password: 'sentinel-password' },
    remember: true,
  })
  expect(call).toHaveBeenCalledTimes(1)
  expect(call.mock.calls[0]![0]).toBe('credentials.submit')
  expect(call.mock.calls[0]![1]).toMatchObject({ values: {} })
  expect(resolve).toHaveBeenCalledWith('provided')
  for (const input of inputs) expect(input).toHaveValue('')
  expect(container.querySelectorAll('input')).toHaveLength(0)
  expect(container.innerHTML).not.toContain('sentinel')
  expect(JSON.stringify(useChat.getState())).toBe(before)
  expect(JSON.stringify(localStorage)).toBe(storage)
  expect(window.location.href).not.toContain('sentinel')
})
it('unlocks the vault and retries the pending secure submission without caching the passphrase', async () => {
  const attempts: { method: string; params: unknown }[] = []
  let locked = true
  const call = vi.spyOn(rpc, 'call').mockImplementation(async (method, params) => {
    attempts.push({ method, params: structuredClone(params) })
    if (method === 'vault.unlock') {
      locked = false
      return { unlocked: true }
    }
    if (locked) throw new RpcError(-32099, 'vault_locked', { code: 'vault_locked' })
    return { ok: true }
  })
  render(<CredentialCard request={request} onResolved={vi.fn()} />)
  fill()
  fireEvent.click(screen.getByRole('button', { name: /^Send$/ }))
  const passphrase = await screen.findByLabelText('Vault passphrase', { exact: true })
  fireEvent.change(passphrase, { target: { value: 'sentinel-passphrase' } })
  fireEvent.click(screen.getByRole('button', { name: /^Unlock$/ }))
  await screen.findByText('Credentials sent securely')
  expect(attempts.map((entry) => entry.method)).toEqual([
    'credentials.submit',
    'vault.unlock',
    'credentials.submit',
  ])
  expect(attempts[2]!.params).toEqual(attempts[0]!.params)
  expect(passphrase).toHaveValue('')
  expect(call.mock.calls[1]![1]).toEqual({ passphrase: '' })
  expect(document.body.innerHTML).not.toContain('sentinel')
})
it('uses generic errors so server error messages cannot echo submitted secrets', async () => {
  vi.spyOn(rpc, 'call').mockRejectedValue(new RpcError(-32603, 'sentinel-password'))
  render(<CredentialCard request={request} onResolved={vi.fn()} />)
  fill()
  fireEvent.click(screen.getByRole('button', { name: /^Send$/ }))
  expect(await screen.findByRole('alert')).toHaveTextContent('Could not complete the request')
  expect(screen.queryByText('sentinel-password')).not.toBeInTheDocument()
})
it('keeps the unlock form available after an incorrect passphrase and clears its input', async () => {
  vi.spyOn(rpc, 'call')
    .mockRejectedValueOnce(new RpcError(-32099, 'vault_locked', { code: 'vault_locked' }))
    .mockRejectedValueOnce(new RpcError(-32603, 'bad phrase'))
  render(<CredentialCard request={request} onResolved={vi.fn()} />)
  fill()
  fireEvent.click(screen.getByRole('button', { name: /^Send$/ }))
  const passphrase = await screen.findByLabelText('Vault passphrase', { exact: true })
  fireEvent.change(passphrase, { target: { value: 'incorrect phrase' } })
  fireEvent.click(screen.getByRole('button', { name: /^Unlock$/ }))
  await screen.findByRole('alert')
  expect(passphrase).toHaveValue('')
  expect(screen.queryByText('Credentials sent securely')).not.toBeInTheDocument()
})
it('cancels through a dedicated RPC and clears values even when cancel fails', async () => {
  const call = vi
    .spyOn(rpc, 'call')
    .mockRejectedValueOnce(new Error('Offline'))
    .mockResolvedValue({})
  const resolve = vi.fn()
  render(<CredentialCard request={request} onResolved={resolve} />)
  fill()
  fireEvent.click(screen.getByRole('button', { name: /^Cancel$/ }))
  await screen.findByRole('alert')
  expect(screen.getByLabelText('Password', { exact: true })).toHaveValue('')
  fireEvent.click(screen.getByRole('button', { name: /^Cancel$/ }))
  await screen.findByText('Cancelled')
  expect(call).toHaveBeenLastCalledWith('credentials.cancel', { request_id: request.id })
  expect(resolve).toHaveBeenCalledWith('cancelled')
})
it.each(['provided', 'cancelled', 'timeout'] as const)(
  'clears detached input nodes when resolved externally: %s',
  async (status) => {
    const { rerender, container } = render(
      <CredentialCard request={request} onResolved={vi.fn()} />,
    )
    fill()
    const inputs = [...container.querySelectorAll('input:not([type=checkbox])')]
    rerender(<CredentialCard request={{ ...request, status }} onResolved={vi.fn()} />)
    await waitFor(() => expect(container.querySelectorAll('input')).toHaveLength(0))
    for (const input of inputs) expect(input).toHaveValue('')
  },
)
it('clears values when navigating away', () => {
  const { unmount } = render(<CredentialCard request={request} onResolved={vi.fn()} />)
  fill()
  const password = screen.getByLabelText('Password', { exact: true })
  unmount()
  expect(password).toHaveValue('')
})
it('never remembers OTPs and masks both OTP and API key inputs', async () => {
  let sent: unknown
  vi.spyOn(rpc, 'call').mockImplementation(async (_method, params) => {
    sent = structuredClone(params)
    return { ok: true }
  })
  render(
    <CredentialCard request={{ ...request, fields: ['otp', 'api_key'] }} onResolved={vi.fn()} />,
  )
  expect(screen.getByLabelText('Remember for this site')).toBeDisabled()
  expect(screen.getByLabelText('Remember for this site')).not.toBeChecked()
  for (const field of ['One-time code', 'API key']) {
    const input = screen.getByLabelText(field, { exact: true })
    expect(input).toHaveAttribute('type', 'password')
    fireEvent.change(input, { target: { value: '123456' } })
  }
  expect(screen.getByLabelText('One-time code', { exact: true })).toHaveAttribute(
    'autocomplete',
    'one-time-code',
  )
  fireEvent.click(screen.getByRole('button', { name: /^Send$/ }))
  await screen.findByText('Credentials sent securely')
  expect(sent).toMatchObject({ remember: false })
})
