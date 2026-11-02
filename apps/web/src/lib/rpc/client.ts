import type { RpcMethods } from './contract'

export class RpcError extends Error {
  readonly code: number
  readonly data?: unknown
  constructor(code: number, message: string, data?: unknown) {
    super(message)
    this.name = 'RpcError'
    this.code = code
    this.data = data
  }
}
export class TransportError extends Error {
  readonly kind: 'network' | 'http' | 'timeout' | 'aborted'
  readonly status?: number
  constructor(message: string, kind: TransportError['kind'], status?: number) {
    super(message)
    this.name = 'TransportError'
    this.kind = kind
    this.status = status
  }
}
export class ProtocolError extends Error {
  constructor(message: string) {
    super(message)
    this.name = 'ProtocolError'
  }
}
export interface CallOptions {
  signal?: AbortSignal
  timeoutMs?: number
}
let nextID = 0
let volatileClientID: string | undefined
function clientID() {
  try {
    const stored = localStorage.getItem('excalibur-client-id')
    if (stored) return stored
  } catch {
    /* A session ID still works when browser storage is unavailable. */
  }
  volatileClientID ??= crypto.randomUUID()
  try {
    localStorage.setItem('excalibur-client-id', volatileClientID)
  } catch {
    /* Optional persistence. */
  }
  return volatileClientID
}
function record(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value)
}
/** A transport only: features own query policies and their wire result types. */
export function createRpcClient(endpoint = '/rpc') {
  return {
    async call<M extends keyof RpcMethods>(
      method: M,
      params?: RpcMethods[M]['params'],
      options: CallOptions = {},
    ): Promise<RpcMethods[M]['result']> {
      const id = String(++nextID)
      const controller = new AbortController()
      let timedOut = false
      const abort = () => controller.abort()
      if (options.signal?.aborted) throw new TransportError('Request aborted', 'aborted')
      options.signal?.addEventListener('abort', abort, { once: true })
      const timer = setTimeout(() => {
        timedOut = true
        controller.abort()
      }, options.timeoutMs ?? 10_000)
      try {
        const response = await fetch(endpoint, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json', 'X-Client-ID': clientID() },
          body: JSON.stringify({
            jsonrpc: '2.0',
            id,
            method,
            ...(params === undefined ? {} : { params }),
          }),
          signal: controller.signal,
        })
        if (!response.ok)
          throw new TransportError(`HTTP ${response.status}`, 'http', response.status)
        let envelope: unknown
        try {
          envelope = await response.json()
        } catch (error) {
          if (controller.signal.aborted) throw error
          throw new ProtocolError('Response is not JSON')
        }
        if (
          !record(envelope) ||
          envelope.jsonrpc !== '2.0' ||
          envelope.id !== id ||
          Object.hasOwn(envelope, 'result') === Object.hasOwn(envelope, 'error')
        )
          throw new ProtocolError('Invalid JSON-RPC response envelope')
        if (Object.hasOwn(envelope, 'error')) {
          const error = envelope.error
          if (!record(error) || !Number.isInteger(error.code) || typeof error.message !== 'string')
            throw new ProtocolError('Invalid JSON-RPC error')
          throw new RpcError(error.code as number, error.message, error.data)
        }
        return envelope.result as RpcMethods[M]['result']
      } catch (error) {
        if (timedOut) throw new TransportError('Request timed out', 'timeout')
        if (controller.signal.aborted) throw new TransportError('Request aborted', 'aborted')
        if (
          error instanceof RpcError ||
          error instanceof ProtocolError ||
          error instanceof TransportError
        )
          throw error
        throw new TransportError('Could not reach the daemon', 'network')
      } finally {
        clearTimeout(timer)
        options.signal?.removeEventListener('abort', abort)
      }
    },
  }
}
export const rpc = createRpcClient()
export const call = rpc.call
