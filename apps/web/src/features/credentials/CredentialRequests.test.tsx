import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import i18n from '@/lib/i18n'
import { rpc } from '@/lib/rpc/client'
import { subscribeEvents } from '@/lib/rpc/events'
import type { ConversationEvent } from '@/lib/rpc/contract'
import type { CredentialRequest } from '@/lib/rpc/credentials'
import { CredentialRequests } from './CredentialRequests'

vi.mock('@/lib/rpc/events', () => ({ subscribeEvents: vi.fn(() => vi.fn()) }))
const request: CredentialRequest = {
  id: 'req-1',
  conversation_id: 'cnv_web_excalibur',
  site: 'example.org',
  purpose: 'Sign in',
  fields: ['password'],
  remember_default: false,
  status: 'pending',
  created_at: '2026-11-03T12:00:00Z',
}
const clients: QueryClient[] = []
beforeEach(async () => {
  await i18n.changeLanguage('en')
})
afterEach(() => {
  cleanup()
  clients.forEach((client) => client.clear())
  vi.restoreAllMocks()
  vi.mocked(subscribeEvents).mockClear()
})
function setup() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  clients.push(client)
  render(
    <QueryClientProvider client={client}>
      <CredentialRequests conversationId={request.conversation_id} />
    </QueryClientProvider>,
  )
  return { client, events: vi.mocked(subscribeEvents).mock.calls.at(-1)![0] }
}
function event(type: string, payload: unknown) {
  return { type, payload } as ConversationEvent
}
it('loads pending metadata, listens for credential events and never caches extra properties or submission values', async () => {
  vi.spyOn(rpc, 'call').mockImplementation(async (method) =>
    method === 'credentials.pending'
      ? { requests: [{ ...request, values: { password: 'server-must-not-send-this' } }] }
      : { ok: true },
  )
  const { client } = setup()
  const password = await screen.findByLabelText('Password', { exact: true })
  expect(
    JSON.stringify(
      client
        .getQueryCache()
        .getAll()
        .map((query) => query.state.data),
    ),
  ).not.toContain('server-must-not-send-this')
  fireEvent.change(password, { target: { value: 'only-in-the-form' } })
  fireEvent.click(screen.getByRole('button', { name: /^Send$/ }))
  await screen.findByText('Credentials sent securely')
  expect(
    JSON.stringify(
      client
        .getQueryCache()
        .getAll()
        .map((query) => query.state.data),
    ),
  ).not.toContain('only-in-the-form')
  expect(client.getMutationCache().getAll()).toHaveLength(0)
})
it('shows requested cards only in their own conversation and collapses on resolution', async () => {
  vi.spyOn(rpc, 'call').mockResolvedValue({ requests: [] })
  const { events } = setup()
  await waitFor(() => expect(rpc.call).toHaveBeenCalled())
  const payload = { ...request, request_id: request.id, run_id: 'run-1' }
  act(() =>
    events.event(event('credentials.requested', { ...payload, conversation_id: 'different' })),
  )
  expect(screen.queryByText('example.org')).not.toBeInTheDocument()
  act(() => events.event(event('credentials.requested', payload)))
  const password = await screen.findByLabelText('Password', { exact: true })
  fireEvent.change(password, { target: { value: 'cleared-on-resolution' } })
  act(() =>
    events.event(event('credentials.resolved', { request_id: request.id, status: 'cancelled' })),
  )
  expect(await screen.findByText('Cancelled')).toBeVisible()
  expect(password).toHaveValue('')
})
it('preserves resolution when pending hydration and requested replay arrive later', async () => {
  let finish: (value: { requests: CredentialRequest[] }) => void = () => {}
  vi.spyOn(rpc, 'call').mockImplementation(
    () =>
      new Promise((resolve) => {
        finish = resolve
      }),
  )
  const { events } = setup()
  act(() =>
    events.event(event('credentials.resolved', { request_id: request.id, status: 'timeout' })),
  )
  await act(async () => finish({ requests: [request] }))
  expect(await screen.findByText('Request expired')).toBeVisible()
  act(() =>
    events.event(
      event('credentials.requested', { ...request, request_id: request.id, run_id: 'run-1' }),
    ),
  )
  expect(screen.getByText('Request expired')).toBeVisible()
  expect(screen.queryByLabelText('Password', { exact: true })).not.toBeInTheDocument()
})
it('refreshes pending requests after a websocket reconnect and exposes a retry on load failure', async () => {
  vi.spyOn(rpc, 'call')
    .mockRejectedValueOnce(new Error('Offline'))
    .mockResolvedValue({ requests: [request] })
  const { events } = setup()
  expect(await screen.findByRole('alert')).toHaveTextContent('Could not load credential requests')
  act(() => events.ready())
  expect(await screen.findByLabelText('Password', { exact: true })).toBeVisible()
})

it('does not reopen a cached resolved card when requested events replay after remounting', async () => {
  vi.spyOn(rpc, 'call').mockResolvedValue({ requests: [] })
  const client = new QueryClient()
  clients.push(client)
  client.setQueryData(
    ['credentials', request.conversation_id],
    [{ ...request, status: 'provided' }],
  )
  render(
    <QueryClientProvider client={client}>
      <CredentialRequests conversationId={request.conversation_id} />
    </QueryClientProvider>,
  )
  const events = vi.mocked(subscribeEvents).mock.calls.at(-1)![0]
  act(() =>
    events.event(
      event('credentials.requested', { ...request, request_id: request.id, run_id: 'run-1' }),
    ),
  )
  expect(await screen.findByText('Credentials sent securely')).toBeVisible()
  expect(screen.queryByLabelText('Password', { exact: true })).not.toBeInTheDocument()
})
it('removes stale pending cards when a reconnect snapshot no longer lists them', async () => {
  vi.spyOn(rpc, 'call')
    .mockResolvedValueOnce({ requests: [request] })
    .mockResolvedValue({ requests: [] })
  const { events } = setup()
  await screen.findByLabelText('Password', { exact: true })
  act(() => events.ready())
  await waitFor(() =>
    expect(screen.queryByLabelText('Password', { exact: true })).not.toBeInTheDocument(),
  )
})
it('retains requested events arriving while an older pending snapshot is loading', async () => {
  let finish: (value: { requests: CredentialRequest[] }) => void = () => {}
  vi.spyOn(rpc, 'call').mockImplementation(
    () =>
      new Promise((resolve) => {
        finish = resolve
      }),
  )
  const { events } = setup()
  act(() =>
    events.event(
      event('credentials.requested', { ...request, request_id: request.id, run_id: 'run-1' }),
    ),
  )
  await act(async () => finish({ requests: [] }))
  expect(await screen.findByLabelText('Password', { exact: true })).toBeVisible()
})
