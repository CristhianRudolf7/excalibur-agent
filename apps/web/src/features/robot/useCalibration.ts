import { useEffect, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { rpc } from '@/lib/rpc/client'
import type { RobotCalibrationStatus } from '@/lib/rpc/contract'
import { subscribeEvents } from '@/lib/rpc/events'

const key = ['robot', 'calibration']
type Action = { type: 'point'; pixel: [number, number] } | { type: 'height' | 'clear' }

export function useCalibration(available: boolean) {
  const client = useQueryClient()
  const [requested, setRequested] = useState(false)
  const [wasAvailable, setWasAvailable] = useState(available)
  // A lost connection or stream must require explicitly re-entering mark mode.
  if (wasAvailable !== available) {
    setWasAvailable(available)
    setRequested(false)
  }
  const locked = useRef(false)
  const query = useQuery({
    queryKey: key,
    queryFn: ({ signal }) => rpc.call('robot.calibration.status', undefined, { signal }),
    retry: false,
    refetchOnWindowFocus: false,
  })
  const mutation = useMutation({
    mutationFn: async (action: Action) => {
      await client.cancelQueries({ queryKey: key })
      if (action.type === 'point')
        return rpc.call('robot.calibration.add_point', { pixel: action.pixel })
      if (action.type === 'height')
        return rpc.call('robot.calibration.set_table_z', { use_current: true })
      return rpc.call('robot.calibration.clear')
    },
    onSuccess: (status) => client.setQueryData(key, status),
    // A rejected or interrupted request may still have reached the daemon.
    onError: () => {
      void client.invalidateQueries({ queryKey: key })
    },
    onSettled: () => {
      locked.current = false
    },
  })
  useEffect(() => {
    if (!requested) return
    const exit = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setRequested(false)
    }
    window.addEventListener('keydown', exit)
    return () => window.removeEventListener('keydown', exit)
  }, [requested])
  useEffect(
    () =>
      subscribeEvents({
        types: ['robot.state'],
        ready: () => {
          void client.invalidateQueries({ queryKey: key })
        },
        offline: () => {
          void client.invalidateQueries({ queryKey: key })
        },
        event: (event) => {
          if (event.type !== 'robot.state') return
          const payload = event.payload as { calibration?: RobotCalibrationStatus }
          if (payload.calibration) client.setQueryData(key, payload.calibration)
        },
      }),
    [client],
  )
  const act = (action: Action) => {
    if (!available || locked.current) return
    locked.current = true
    if (action.type === 'clear') setRequested(false)
    mutation.mutate(action)
  }
  return {
    query,
    mutation,
    available,
    marking: available && requested,
    toggle: () => setRequested((value) => available && !value),
    exit: () => setRequested(false),
    addPoint: (pixel: [number, number]) => act({ type: 'point', pixel }),
    setHeight: () => act({ type: 'height' }),
    clear: () => act({ type: 'clear' }),
  }
}
