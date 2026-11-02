import { useQuery } from '@tanstack/react-query'
import { rpc, TransportError } from '@/lib/rpc/client'

export const profileQueryOptions = {
  queryKey: ['principal'] as const,
  queryFn: ({ signal }: { signal: AbortSignal }) =>
    rpc.call('principal.get', undefined, { signal }),
  retryOnMount: false,
  staleTime: 5 * 60_000,
  retry: (count: number, error: Error) =>
    error instanceof TransportError && error.kind !== 'aborted' && count < 1,
}
export function useProfile() {
  return useQuery(profileQueryOptions)
}
