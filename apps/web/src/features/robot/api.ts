import { useEffect } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { rpc } from '@/lib/rpc/client'
import { subscribeEvents } from '@/lib/rpc/events'
import { robotEventState, type RobotStatus } from './types'

const key = ['robot', 'status']

export function useRobot() {
  const client = useQueryClient()
  const query = useQuery({
    queryKey: key,
    queryFn: ({ signal }) => rpc.call('robot.status', {}, { signal }),
    retry: false,
    refetchOnWindowFocus: false,
  })
  const connection = useMutation({
    mutationFn: async (host: string | null) => {
      await client.cancelQueries({ queryKey: key })
      return host === null
        ? rpc.call('robot.disconnect', {})
        : rpc.call('robot.connect', { host }, { timeoutMs: 35_000 })
    },
    onSuccess: (status) => client.setQueryData(key, status),
  })
  useEffect(
    () =>
      subscribeEvents({
        types: ['robot.state'],
        ready: () => void client.invalidateQueries({ queryKey: key }),
        offline: () => void client.invalidateQueries({ queryKey: key }),
        event: (event) => {
          if (event.type !== 'robot.state') return
          const state = robotEventState(event.payload)
          if (state)
            client.setQueryData<RobotStatus>(key, (previous) => ({
              ...previous,
              bridge: 'running',
              state,
            }))
        },
      }),
    [client],
  )
  return { query, connection }
}
