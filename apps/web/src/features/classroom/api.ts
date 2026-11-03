import { useEffect } from 'react'
import { useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query'
import { rpc } from '@/lib/rpc/client'
import { subscribeEvents } from '@/lib/rpc/events'
import type { ConversationEvent } from '@/lib/rpc/contract'
import { classroomEventTypes, type ClassroomEventPayload } from '@/lib/rpc/classroom'
import { receiveClassroomEvent, useClassroom } from './store'
export const classroomKey = ['classroom'] as const
export const useCourses = () =>
  useQuery({
    queryKey: [...classroomKey, 'courses'],
    queryFn: () => rpc.call('classroom.list'),
  })
export const useCourse = (courseId?: string) =>
  useQuery({
    queryKey: [...classroomKey, 'course', courseId],
    queryFn: () => rpc.call('classroom.get', { course_id: courseId! }),
    enabled: Boolean(courseId),
  })
export function applyClassroomEvent(
  client: QueryClient,
  event: Pick<ConversationEvent, 'type' | 'payload'>,
) {
  if (!classroomEventTypes.some((type) => type === event.type)) return
  receiveClassroomEvent(event.type, event.payload as ClassroomEventPayload)
  void client.invalidateQueries({ queryKey: classroomKey })
}
export function useClassroomEvents() {
  const client = useQueryClient()
  useEffect(() => {
    let live = false
    return subscribeEvents({
      types: [...classroomEventTypes],
      ready: () => {
        live = true
        void client.invalidateQueries({ queryKey: classroomKey })
      },
      offline: () => {
        live = false
      },
      event: (event) => {
        applyClassroomEvent(client, event)
        const payload = event.payload as ClassroomEventPayload
        if (live && event.type === 'classroom.course_created' && payload.course_id)
          useClassroom.getState().select(payload.course_id)
      },
    })
  }, [client])
}
