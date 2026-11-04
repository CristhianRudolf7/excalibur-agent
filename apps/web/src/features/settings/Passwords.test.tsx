import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import i18n from '@/lib/i18n'
import { rpc, RpcError } from '@/lib/rpc/client'
import { Passwords } from './Passwords'
import { loginAddress, loginCards } from '@/lib/rpc/credentials'

const entry = {
  id: 'site-1',
  name: 'example-login',
  type: 'website_password',
  domains: ['example.org'],
  metadata: {},
  created_at: '2026-10-30T12:00:00Z',
  updated_at: '2026-11-03T12:00:00Z',
}
const clients: QueryClient[] = []
beforeEach(async () => {
  await i18n.changeLanguage('en')
  HTMLDialogElement.prototype.showModal = vi.fn(function (this: HTMLDialogElement) {
    this.setAttribute('open', '')
  })
  HTMLDialogElement.prototype.close = vi.fn()
})
afterEach(() => {
  cleanup()
  clients.forEach((client) => client.clear())
  vi.restoreAllMocks()
})
function setup() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  clients.push(client)
  render(
    <QueryClientProvider client={client}>
      <Passwords />
    </QueryClientProvider>,
  )
  return client
}
it('lists remembered site metadata and dates without rendering other secrets or ephemeral entries', async () => {
  vi.spyOn(rpc, 'call').mockResolvedValue({
    entries: [
      entry,
      { ...entry, id: 'ephemeral', name: 'ephemeral-login', expires_at: '2026-11-03T13:00:00Z' },
      {
        ...entry,
        id: 'provider',
        name: 'provider-token',
        type: 'provider_credential',
        domains: [],
      },
    ],
    locked: false,
  })
  setup()
  expect(await screen.findByText('example-login')).toBeVisible()
  expect(screen.getByText('example.org')).toBeVisible()
  expect(screen.getByText(/^Created:/)).toBeVisible()
  expect(screen.getByText(/^Updated:/)).toBeVisible()
  expect(screen.getByText('Vault unlocked')).toBeVisible()
  expect(screen.queryByText('ephemeral-login')).not.toBeInTheDocument()
  expect(screen.queryByText('provider-token')).not.toBeInTheDocument()
  expect(screen.queryByRole('textbox')).not.toBeInTheDocument()
})
it('requires a confirmation before deleting and refreshes the metadata list', async () => {
  let entries = [entry]
  const call = vi.spyOn(rpc, 'call').mockImplementation(async (method) => {
    if (method === 'vault.delete') {
      entries = []
      return { deleted: entry.name }
    }
    return { entries, locked: false }
  })
  setup()
  fireEvent.click(await screen.findByRole('button', { name: 'Delete example-login' }))
  expect(call).not.toHaveBeenCalledWith('vault.delete', expect.anything())
  fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Cancel' }))
  expect(call).not.toHaveBeenCalledWith('vault.delete', expect.anything())
  fireEvent.click(screen.getByRole('button', { name: 'Delete example-login' }))
  fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: /^Delete$/ }))
  expect(await screen.findByText('No remembered sites.')).toBeVisible()
  expect(call).toHaveBeenCalledWith('vault.delete', { name: entry.name, confirmed: true })
})
it('unlocks and locks the vault through separate secure calls and clears the passphrase', async () => {
  let locked = true
  const call = vi.spyOn(rpc, 'call').mockImplementation(async (method) => {
    if (method === 'vault.unlock') {
      locked = false
      return { unlocked: true }
    }
    if (method === 'vault.lock') {
      locked = true
      return { locked: true }
    }
    return { entries: [entry], locked }
  })
  const client = setup()
  await screen.findByText('Vault locked')
  fireEvent.click(screen.getByRole('button', { name: /^Unlock$/ }))
  const input = screen.getByLabelText('Vault passphrase', { exact: true })
  fireEvent.change(input, { target: { value: 'only-unlock-call' } })
  fireEvent.submit(input.closest('form')!)
  await screen.findByText('Vault unlocked')
  expect(input).toHaveValue('')
  expect(JSON.stringify(client.getQueryData(['vault-metadata']))).not.toContain('only-unlock-call')
  expect(client.getMutationCache().getAll()).toHaveLength(0)
  fireEvent.click(screen.getByRole('button', { name: /^Lock$/ }))
  await screen.findByText('Vault locked')
  expect(call).toHaveBeenCalledWith('vault.lock', {})
})
it('does not infer unlocked state from a successful legacy metadata listing', async () => {
  vi.spyOn(rpc, 'call').mockResolvedValue({ entries: [entry] })
  setup()
  await screen.findByText(entry.name)
  expect(screen.getByText('Vault status unavailable')).toBeVisible()
  expect(screen.queryByText('Vault unlocked')).not.toBeInTheDocument()
})
it('keeps the entry and shows a generic error when deletion fails', async () => {
  vi.spyOn(rpc, 'call').mockImplementation(async (method) => {
    if (method === 'vault.delete') throw new RpcError(-32603, 'private-server-detail')
    return { entries: [entry], locked: false }
  })
  setup()
  fireEvent.click(await screen.findByRole('button', { name: 'Delete example-login' }))
  fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: /^Delete$/ }))
  await screen.findByRole('alert')
  expect(screen.getByText(entry.name)).toBeVisible()
  expect(screen.queryByText('private-server-detail')).not.toBeInTheDocument()
})
it('allows retry after a failed metadata load', async () => {
  vi.spyOn(rpc, 'call')
    .mockRejectedValueOnce(new Error('Offline'))
    .mockResolvedValue({ entries: [], locked: true })
  setup()
  await screen.findByRole('alert')
  fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
  await waitFor(() => expect(screen.getByText('No remembered sites.')).toBeVisible())
})

const bundledEntries = ['username', 'password'].map((field) => ({
  ...entry,
  id: `bundle-${field}`,
  name: `credentials-bundle-${field}`,
  metadata: {
    credential_bundle: 'credentials-bundle',
    credential_site: 'accounts.example.org',
    credential_label: 'Office',
    credential_field: field,
  },
  domains: ['accounts.example.org'],
}))
it('derives exact hosts and preserves the full start URL', () => {
  expect(loginAddress(' HTTPS://START.Example.org:8443/start ')).toEqual({
    site: 'start.example.org',
    start_url: 'HTTPS://START.Example.org:8443/start',
  })
  expect(loginAddress('START.Example.org:8443/path')).toEqual({
    site: 'start.example.org',
    start_url: '',
  })
  expect(
    loginAddress('https://start.example.org/', 'https://ACCOUNTS.example.org:443/sign-in'),
  ).toEqual({ site: 'accounts.example.org', start_url: 'https://start.example.org/' })
  expect(loginAddress('http://[::1]:8080/')).toEqual({
    site: '::1',
    start_url: 'http://[::1]:8080/',
  })
  for (const address of [
    '',
    'not a host',
    'ftp://example.org',
    'https://user@example.org',
    'https://:443',
  ])
    expect(() => loginAddress(address)).toThrow()
})
it('groups a bundle into one card and deletes all its fields after confirmation', async () => {
  let entries = bundledEntries
  const deleted: string[] = []
  vi.spyOn(rpc, 'call').mockImplementation(async (method, params) => {
    if (method === 'vault.delete') {
      const name = (params as { name: string }).name
      deleted.push(name)
      entries = entries.filter((entry) => entry.name !== name)
      return { deleted: name }
    }
    return { entries, locked: false }
  })
  expect(loginCards(entries)).toHaveLength(1)
  setup()
  await screen.findByText('Office')
  expect(screen.getAllByText('accounts.example.org')).toHaveLength(1)
  expect(screen.getByText('Username saved')).toBeVisible()
  fireEvent.click(screen.getByRole('button', { name: 'Delete Office' }))
  fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: /^Delete$/ }))
  await screen.findByText('No remembered sites.')
  expect(deleted).toEqual(bundledEntries.map((entry) => entry.name))
})
function fillLogin() {
  for (const [label, value] of [
    ['Name (optional)', 'Office'],
    ['Address', 'https://start.example.org:8443/home'],
    ['Login host (optional)', 'ACCOUNTS.example.org/login'],
    ['Username', 'settings-user-sentinel'],
    ['Password', 'settings-password-sentinel'],
  ]) {
    fireEvent.change(screen.getByLabelText(label!, { exact: true }), { target: { value } })
  }
}
it('saves outside query state and clears secrets before the call completes', async () => {
  let entries: typeof bundledEntries = []
  let finish: ((value: { ok: boolean }) => void) | undefined
  let submitted: unknown
  vi.spyOn(rpc, 'call').mockImplementation(async (method, params) => {
    if (method === 'credentials.save_login') {
      submitted = structuredClone(params)
      return new Promise<{ ok: boolean }>((resolve) => {
        finish = resolve
      })
    }
    return { entries, locked: false }
  })
  const client = setup()
  fireEvent.click(await screen.findByRole('button', { name: 'Add login' }))
  fillLogin()
  const password = screen.getByLabelText('Password', { exact: true })
  fireEvent.submit(password.closest('form')!)
  expect(password).toHaveValue('')
  expect(screen.getByLabelText('Username', { exact: true })).toHaveValue('')
  expect(submitted).toEqual({
    site: 'accounts.example.org',
    label: 'Office',
    start_url: 'https://start.example.org:8443/home',
    values: { username: 'settings-user-sentinel', password: 'settings-password-sentinel' },
  })
  expect(JSON.stringify(client.getQueryData(['vault-metadata']))).not.toContain(
    'settings-password-sentinel',
  )
  expect(client.getMutationCache().getAll()).toHaveLength(0)
  entries = bundledEntries
  finish?.({ ok: true })
  await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  await screen.findByText('Office')
  expect(document.body.innerHTML).not.toContain('settings-password-sentinel')
  fireEvent.click(screen.getByRole('button', { name: 'Add login' }))
  expect(screen.getByLabelText('Password', { exact: true })).toHaveValue('')
})
it('retains only non-secret fields on a locked save and allows unlock', async () => {
  let locked = false
  vi.spyOn(rpc, 'call').mockImplementation(async (method) => {
    if (method === 'credentials.save_login') {
      locked = true
      throw new RpcError(-32099, 'vault_locked')
    }
    if (method === 'vault.unlock') {
      locked = false
      return { unlocked: true }
    }
    return { entries: [], locked }
  })
  setup()
  fireEvent.click(await screen.findByRole('button', { name: 'Add login' }))
  fillLogin()
  const password = screen.getByLabelText('Password', { exact: true })
  fireEvent.submit(password.closest('form')!)
  const dialog = within(screen.getByRole('dialog'))
  const phrase = await dialog.findByLabelText('Vault passphrase', { exact: true })
  expect(dialog.getByLabelText('Name (optional)')).toHaveValue('Office')
  expect(dialog.getByLabelText('Address')).toHaveValue('https://start.example.org:8443/home')
  expect(dialog.getByLabelText('Login host (optional)')).toHaveValue('ACCOUNTS.example.org/login')
  expect(dialog.queryByLabelText('Password', { exact: true })).not.toBeInTheDocument()
  expect(document.body.innerHTML).not.toContain('settings-password-sentinel')
  fireEvent.change(phrase, { target: { value: 'unlock-only-sentinel' } })
  fireEvent.submit(phrase.closest('form')!)
  await waitFor(() => expect(dialog.getByLabelText('Password', { exact: true })).toHaveValue(''))
  expect(dialog.getByLabelText('Username', { exact: true })).toHaveValue('')
  expect(phrase).toHaveValue('')
})
it('clears inputs on validation and network errors and when closing', async () => {
  const call = vi.spyOn(rpc, 'call').mockImplementation(async (method) => {
    if (method === 'credentials.save_login') throw new Error('private-save-detail')
    return { entries: [], locked: false }
  })
  setup()
  fireEvent.click(await screen.findByRole('button', { name: 'Add login' }))
  fillLogin()
  const password = screen.getByLabelText('Password', { exact: true })
  fireEvent.change(screen.getByLabelText('Address'), { target: { value: 'ftp://example.org' } })
  fireEvent.submit(password.closest('form')!)
  await screen.findByText('Enter a valid address or host.')
  expect(password).toHaveValue('')
  expect(call.mock.calls.some(([method]) => method === 'credentials.save_login')).toBe(false)
  fillLogin()
  fireEvent.submit(password.closest('form')!)
  await waitFor(() => expect(within(screen.getByRole('dialog')).getByRole('alert')).toBeVisible())
  expect(password).toHaveValue('')
  expect(screen.queryByText('private-save-detail')).not.toBeInTheDocument()
  fireEvent.change(password, { target: { value: 'closing-sentinel' } })
  fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Cancel' }))
  expect(password).toHaveValue('')
  fireEvent.click(screen.getByRole('button', { name: 'Add login' }))
  expect(screen.getByLabelText('Password', { exact: true })).toHaveValue('')
})
