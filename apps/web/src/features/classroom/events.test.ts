import { QueryClient } from '@tanstack/react-query'
import { beforeEach, expect, it, vi } from 'vitest'
import { applyClassroomEvent } from './api'
import { useClassroom, targetKey } from './store'
beforeEach(() =>
  useClassroom.setState({
    courseId: undefined,
    topicId: undefined,
    preparing: {},
    requesting: {},
    quizzes: {},
  }),
)
it('handles readiness by target and refreshes persisted course, quiz and feedback data', () => {
  const client = new QueryClient()
  const invalidate = vi.spyOn(client, 'invalidateQueries')
  const payload = { course_id: 'course-1', topic_id: 'topic-2' }
  const event = (type: string, extra = {}) =>
    applyClassroomEvent(client, { type, payload: { ...payload, ...extra } })
  event('classroom.class_requested')
  expect(useClassroom.getState().preparing[targetKey('course-1', 'topic-2')]).toBe(true)
  event('classroom.class_ready')
  expect(useClassroom.getState().preparing[targetKey('course-1', 'topic-2')]).toBe(false)
  event('classroom.quiz_requested')
  expect(useClassroom.getState().requesting['course-1:topic-2']).toBe(true)
  event('classroom.quiz_ready', { quiz_id: 'quiz-2' })
  expect(useClassroom.getState().requesting['course-1:topic-2']).toBe(false)
  expect(useClassroom.getState().quizzes['course-1:topic-2']).toBe('quiz-2')
  event('classroom.quiz_ready', { topic_id: undefined, final: true, quiz_id: 'final-1' })
  expect(useClassroom.getState().quizzes['course-1:final']).toBe('final-1')
  for (const type of [
    'course_created',
    'course_updated',
    'attempt_graded',
    'feedback_ready',
    'topic_completed',
    'course_completed',
  ])
    event(`classroom.${type}`)
  expect(invalidate).toHaveBeenCalledTimes(11)
  expect(invalidate).toHaveBeenLastCalledWith({ queryKey: ['classroom'] })
  event('unrelated.event')
  expect(invalidate).toHaveBeenCalledTimes(11)
  expect(useClassroom.getState().courseId).toBeUndefined()
})
