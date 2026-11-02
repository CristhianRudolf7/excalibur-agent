import { useQuery } from '@tanstack/react-query'
import { rpc } from '@/lib/rpc/client'

export function useAutomationDetail(id: string) {
  return useQuery({
    queryKey: ['automation', id],
    queryFn: ({ signal }) => rpc.call('automations.get', { id }, { signal }),
    retry: false,
  })
}
export function useAutomationRuns(id: string) {
  return useQuery({
    queryKey: ['automation-runs', id],
    queryFn: ({ signal }) => rpc.call('automations.runs', { id }, { signal }),
    retry: false,
  })
}
