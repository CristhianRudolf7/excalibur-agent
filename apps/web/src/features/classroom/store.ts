import { create } from 'zustand'
import type { ClassroomEventPayload } from '@/lib/rpc/classroom'
export const targetKey = (courseId: string, topicId?: string) => `${courseId}:${topicId ?? 'final'}`
interface State {
  quizOpen: boolean
  courseId?: string
  topicId?: string
  preparing: Record<string, boolean>
  quizzes: Record<string, string | undefined>
  requesting: Record<string, boolean>
  select: (courseId?: string, topicId?: string) => void
}
export const useClassroom = create<State>((set) => ({
  quizOpen: false,
  preparing: {},
  quizzes: {},
  requesting: {},
  select: (courseId, topicId) => set({ courseId, topicId, quizOpen: false }),
}))
export function receiveClassroomEvent(type: string, payload: ClassroomEventPayload) {
  if (!type.startsWith('classroom.') || !payload.course_id) return
  const key = targetKey(payload.course_id, payload.topic_id)
  if (type === 'classroom.class_requested' || type === 'classroom.class_ready')
    useClassroom.setState((s) => ({
      preparing: { ...s.preparing, [key]: type.endsWith('_requested') },
    }))
  if (type === 'classroom.quiz_requested' || type === 'classroom.quiz_ready')
    useClassroom.setState((s) => ({
      requesting: { ...s.requesting, [key]: type.endsWith('_requested') },
      ...(type === 'classroom.quiz_requested'
        ? { quizzes: { ...s.quizzes, [key]: undefined } }
        : {}),
      ...(payload.quiz_id && type === 'classroom.quiz_ready'
        ? { quizzes: { ...s.quizzes, [key]: payload.quiz_id } }
        : {}),
    }))
}
