import { expect, it } from 'vitest'
import { ProtocolError, RpcError, TransportError } from '@/lib/rpc/client'
import { transientViewerError, viewerRetryDelay } from './retry'

it('caps exponential retry delays at fifteen seconds', () => {
  expect(Array.from({ length: 8 }, (_, i) => viewerRetryDelay(i))).toEqual([
    1000, 2000, 4000, 8000, 15_000, 15_000, 15_000, 15_000,
  ])
})
it('retries conflicts, unavailable runtimes and temporary transport errors', () => {
  for (const error of [
    new RpcError(-32005, 'busy'),
    new RpcError(-32020, 'unavailable'),
    new RpcError(-32603, 'internal'),
    new TransportError('offline', 'network'),
    new TransportError('slow', 'timeout'),
    ...[408, 429, 500, 503].map((status) => new TransportError('http', 'http', status)),
  ])
    expect(transientViewerError(error)).toBe(true)
})
it('preserves permanent, aborted and malformed-response errors', () => {
  for (const error of [
    new RpcError(-32001, 'missing'),
    new RpcError(-32602, 'invalid params'),
    new TransportError('aborted', 'aborted'),
    ...[400, 401, 403, 404].map((status) => new TransportError('http', 'http', status)),
    new ProtocolError('invalid response'),
    new Error('invalid mesh'),
  ])
    expect(transientViewerError(error)).toBe(false)
})
