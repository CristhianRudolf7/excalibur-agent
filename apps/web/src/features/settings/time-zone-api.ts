import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { rpc } from '@/lib/rpc/client'
import type { RpcMethods } from '@/lib/rpc/contract'
import { profileQueryOptions } from '@/features/profile/api'
import { browserTimeZone } from './time-zone'

export function useUpdateTimeZone() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: (params: RpcMethods['principal.update']['params']) =>
      rpc.call('principal.update', params),
    onMutate: () => client.cancelQueries({ queryKey: profileQueryOptions.queryKey }),
    onSuccess: async (profile) => {
      await client.cancelQueries({ queryKey: profileQueryOptions.queryKey })
      client.setQueryData(profileQueryOptions.queryKey, profile)
    },
  })
}

/** One report per app startup, including across StrictMode and route remounts. */
export function BrowserTimeZoneDetection() {
  const client = useQueryClient()
  useQuery({
    queryKey: ['browser-time-zone'],
    queryFn: async () => {
      // Deliberately survives component unmount: the cached startup request must
      // run once. The daemon atomically protects a stored manual preference.
      await rpc.call('principal.update', { timezone: browserTimeZone(), timezone_source: 'auto' })
      await client.invalidateQueries({ queryKey: profileQueryOptions.queryKey })
      return true
    },
    staleTime: Infinity,
    gcTime: Infinity,
    retry: false,
    refetchOnMount: false,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  })
  return null
}
