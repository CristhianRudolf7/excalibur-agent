import { useQuery } from '@tanstack/react-query'
import { labCall, useRobotEvent } from './api'
import type { Session } from './types'

export function useSessionLog(session: Session, attempt: number) {
  const log = useQuery({
    queryKey: ['robotlab', 'log', session.id, attempt],
    queryFn: ({ signal }) =>
      labCall('robotlab.session.log', { session: session.id, attempt }, { signal }),
    refetchInterval: session.status === 'live' ? 2000 : false,
  })
  useRobotEvent('robotlab.log', ({ entry }) => {
    if (entry.session === session.id && entry.attempt === attempt) void log.refetch()
  })
  return log
}
