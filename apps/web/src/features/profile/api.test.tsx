import { afterEach, expect, it, vi } from 'vitest'
import { cleanup, renderHook, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { rpc, RpcError, TransportError } from '@/lib/rpc/client'
import { profileQueryOptions, useProfile } from './api'
import { initials } from './initials'

const principal = { id: 'local', name: 'Ana Prueba', timezone: 'UTC', language: 'es' }
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})
function setup() {
  const client = new QueryClient({ defaultOptions: { queries: { retryDelay: 0 } } })
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  )
  return { client, wrapper }
}
it('loads and caches a shared profile for five minutes', async () => {
  const call = vi.spyOn(rpc, 'call').mockResolvedValue(principal)
  const { wrapper, client } = setup()
  const first = renderHook(useProfile, { wrapper })
  await waitFor(() => expect(first.result.current.data).toEqual(principal))
  const second = renderHook(useProfile, { wrapper })
  await waitFor(() => expect(second.result.current.data).toEqual(principal))
  expect(call).toHaveBeenCalledTimes(1)
  expect(call).toHaveBeenCalledWith('principal.get', undefined, { signal: expect.any(AbortSignal) })
  expect(profileQueryOptions.staleTime).toBe(300_000)
  expect(client.getQueryData(['principal'])).toEqual(principal)
  client.clear()
})
it('retries one transport failure and succeeds', async () => {
  const call = vi
    .spyOn(rpc, 'call')
    .mockRejectedValueOnce(new TransportError('Offline', 'network'))
    .mockResolvedValue(principal)
  const { wrapper, client } = setup()
  const hook = renderHook(useProfile, { wrapper })
  await waitFor(() => expect(hook.result.current.isSuccess).toBe(true))
  expect(call).toHaveBeenCalledTimes(2)
  client.clear()
})
it('does not retry RPC errors and permits an explicit refetch', async () => {
  const call = vi
    .spyOn(rpc, 'call')
    .mockRejectedValueOnce(new RpcError(-32603, 'Failed'))
    .mockResolvedValue(principal)
  const { wrapper, client } = setup()
  const hook = renderHook(useProfile, { wrapper })
  await waitFor(() => expect(hook.result.current.isError).toBe(true))
  expect(call).toHaveBeenCalledTimes(1)
  await hook.result.current.refetch()
  await waitFor(() => expect(hook.result.current.data).toEqual(principal))
  expect(call).toHaveBeenCalledTimes(2)
  client.clear()
})
it('stops retrying after one additional transport attempt', async () => {
  const call = vi.spyOn(rpc, 'call').mockRejectedValue(new TransportError('Offline', 'timeout'))
  const { wrapper, client } = setup()
  const hook = renderHook(useProfile, { wrapper })
  await waitFor(() => expect(hook.result.current.isError).toBe(true))
  expect(call).toHaveBeenCalledTimes(2)
  client.clear()
})
it.each([
  ['Ana Prueba', 'AP'],
  ['  Ana María Prueba ', 'AP'],
  ['Ana', 'A'],
  ['', ''],
  ['Álvaro Núñez', 'ÁN'],
])('derives initials for %s', (name, expected) => {
  expect(initials(name)).toBe(expected)
})
