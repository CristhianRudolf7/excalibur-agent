import { afterEach, expect, it, vi } from 'vitest'
import { vaultLocked } from '@/features/credentials/secure'
import { createRpcClient, ProtocolError, RpcError, TransportError } from './client'

const principal = { id: 'local', name: 'Ana Prueba', timezone: 'UTC', language: 'es' }
function reply(payload: (id: string) => unknown) {
  const fetch = vi.fn(async (_url: unknown, init?: RequestInit) => {
    const request = JSON.parse(init!.body as string) as { id: string }
    return new Response(JSON.stringify(payload(request.id)))
  })
  vi.stubGlobal('fetch', fetch)
  return fetch
}
afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
  vi.useRealTimers()
  localStorage.clear()
})

it('posts typed JSON-RPC with increasing string IDs and a persisted client ID', async () => {
  const fetch = reply((id) => ({ jsonrpc: '2.0', id, result: principal }))
  const client = createRpcClient()
  expect(await client.call('principal.get')).toEqual(principal)
  await createRpcClient().call('principal.get')
  const [url, first] = fetch.mock.calls[0]!
  const second = fetch.mock.calls[1]![1]!
  expect(url).toBe('/rpc')
  expect(first?.method).toBe('POST')
  const a = JSON.parse(first!.body as string),
    b = JSON.parse(second.body as string)
  expect(a).toMatchObject({ jsonrpc: '2.0', method: 'principal.get' })
  expect(typeof a.id).toBe('string')
  expect(Number(b.id)).toBe(Number(a.id) + 1)
  expect((first!.headers as Record<string, string>)['X-Client-ID']).toBe(
    localStorage.getItem('excalibur-client-id'),
  )
  expect(second.headers).toEqual(first!.headers)
})
it('preserves typed server errors and data', async () => {
  reply((id) => ({
    jsonrpc: '2.0',
    id,
    error: { code: -32603, message: 'Failed', data: { retry: false } },
  }))
  await expect(createRpcClient().call('principal.get')).rejects.toMatchObject({
    name: 'RpcError',
    code: -32603,
    message: 'Failed',
    data: { retry: false },
  })
  expect(new RpcError(1, 'error')).toBeInstanceOf(Error)
})
it('classifies HTTP and network failures as transport errors', async () => {
  vi.stubGlobal(
    'fetch',
    vi.fn(async () => new Response('', { status: 503 })),
  )
  await expect(createRpcClient().call('principal.get')).rejects.toMatchObject({
    name: 'TransportError',
    kind: 'http',
    status: 503,
  })
  vi.stubGlobal(
    'fetch',
    vi.fn(async () => {
      throw new TypeError('Network unavailable')
    }),
  )
  await expect(createRpcClient().call('principal.get')).rejects.toMatchObject({
    name: 'TransportError',
    kind: 'network',
  })
  expect(new TransportError('error', 'network')).toBeInstanceOf(Error)
})
it.each([
  () => null,
  (id: string) => ({ jsonrpc: '1.0', id, result: principal }),
  () => ({ jsonrpc: '2.0', id: 'wrong', result: principal }),
  (id: string) => ({ jsonrpc: '2.0', id }),
  (id: string) => ({
    jsonrpc: '2.0',
    id,
    result: principal,
    error: { code: 1, message: 'Invalid' },
  }),
  (id: string) => ({ jsonrpc: '2.0', id, error: { code: '1', message: 'Invalid' } }),
])('rejects malformed envelopes', async (payload) => {
  reply(payload)
  await expect(createRpcClient().call('principal.get')).rejects.toBeInstanceOf(ProtocolError)
})
it('rejects non-JSON responses', async () => {
  vi.stubGlobal(
    'fetch',
    vi.fn(async () => new Response('not json')),
  )
  await expect(createRpcClient().call('principal.get')).rejects.toBeInstanceOf(ProtocolError)
})
function pendingFetch() {
  vi.stubGlobal(
    'fetch',
    vi.fn(
      (_url: unknown, options: RequestInit) =>
        new Promise((_resolve, reject) => {
          options.signal!.addEventListener(
            'abort',
            () => reject(new DOMException('Aborted', 'AbortError')),
            { once: true },
          )
        }),
    ),
  )
}
it('times out at the default 10 second deadline and supports custom deadlines', async () => {
  vi.useFakeTimers()
  pendingFetch()
  const result = expect(createRpcClient().call('principal.get')).rejects.toMatchObject({
    kind: 'timeout',
  })
  await vi.advanceTimersByTimeAsync(10_000)
  await result
  const custom = expect(
    createRpcClient().call('principal.get', undefined, { timeoutMs: 25 }),
  ).rejects.toMatchObject({ kind: 'timeout' })
  await vi.advanceTimersByTimeAsync(25)
  await custom
  expect(vi.getTimerCount()).toBe(0)
})
it('propagates cancellation before or during a request and releases its timeout', async () => {
  vi.useFakeTimers()
  pendingFetch()
  const controller = new AbortController()
  const result = expect(
    createRpcClient().call('principal.get', undefined, { signal: controller.signal }),
  ).rejects.toMatchObject({ kind: 'aborted' })
  controller.abort()
  await result
  await expect(
    createRpcClient().call('principal.get', undefined, { signal: controller.signal }),
  ).rejects.toMatchObject({ kind: 'aborted' })
  expect(vi.getTimerCount()).toBe(0)
})

it('preserves the numeric credential error and recognizes data.code for unlock and retry', async () => {
  reply((id) => ({
    jsonrpc: '2.0',
    id,
    error: { code: -32099, message: 'Unlock required', data: { code: 'vault_locked' } },
  }))
  const error = await createRpcClient()
    .call('credentials.submit', {
      request_id: 'pending-request',
      values: {},
      remember: true,
    })
    .catch((error: unknown) => error)
  expect(error).toBeInstanceOf(RpcError)
  expect(error).toMatchObject({ code: -32099, data: { code: 'vault_locked' } })
  expect(vaultLocked(error)).toBe(true)
  expect(vaultLocked(new RpcError(-32099, 'Unavailable'))).toBe(false)
})
