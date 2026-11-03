import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import i18n from '@/lib/i18n'
import { rpc, RpcError } from '@/lib/rpc/client'
import { Passwords } from './Passwords'

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
