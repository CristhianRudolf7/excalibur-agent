import { RpcError, TransportError } from '@/lib/rpc/client'

export function transientViewerError(error: unknown): boolean {
  if (error instanceof RpcError) return [-32005, -32020, -32603].includes(error.code)
  if (error instanceof TransportError)
    return (
      error.kind === 'network' ||
      error.kind === 'timeout' ||
      (error.kind === 'http' &&
        (error.status === 408 || error.status === 429 || (error.status ?? 0) >= 500))
    )
  return false
}

export function viewerRetryDelay(attempt: number) {
  return Math.min(1000 * 2 ** Math.min(attempt, 4), 15_000)
}

export function waitForViewerRetry(attempt: number, signal: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    const abort = () => {
      clearTimeout(timer)
      signal.removeEventListener('abort', abort)
      reject(new TransportError('Request aborted', 'aborted'))
    }
    const timer = setTimeout(() => {
      signal.removeEventListener('abort', abort)
      resolve()
    }, viewerRetryDelay(attempt))
    signal.addEventListener('abort', abort, { once: true })
    if (signal.aborted) abort()
  })
}
