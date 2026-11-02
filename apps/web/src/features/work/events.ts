import { useEffect } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { subscribeEvents } from '@/lib/rpc/events'
import type { Reminder } from '@/lib/rpc/contract'

export function useWorkEvents(onFired?: (reminder: Reminder) => void) {
  const client = useQueryClient()
  useEffect(() => {
    if (import.meta.env.EXCALIBUR_WEB_WORK_EVENTS === '0') return
    let connected = false
    return subscribeEvents({
      types: ['task.updated', 'reminder.updated', 'reminder.fired'],
      ready: () => {
        // Mounted queries fetch initially; refresh them when a socket reconnects.
        if (connected) void client.invalidateQueries({ queryKey: ['work'] })
        connected = true
      },
      offline: () => {},
      event: (event) => {
        if (event.type === 'task.updated') {
          void client.invalidateQueries({ queryKey: ['work', 'tasks'] })
          void client.invalidateQueries({ queryKey: ['work', 'task'] })
        }
        if (event.type === 'reminder.updated' || event.type === 'reminder.fired') {
          void client.invalidateQueries({ queryKey: ['work', 'reminders'] })
          if (
            event.type === 'reminder.fired' &&
            'reminder' in event.payload &&
            event.payload.reminder
          )
            onFired?.(event.payload.reminder)
        }
      },
    })
  }, [client, onFired])
}
