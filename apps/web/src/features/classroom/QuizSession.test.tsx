import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import i18n from '@/lib/i18n'
import { rpc } from '@/lib/rpc/client'
import type { Attempt, Course, QuizView } from '@/lib/rpc/classroom'
import { QuizSession } from './QuizSession'
import { courseFixture } from './fixtures'
import { useClassroom } from './store'
import { applyClassroomEvent } from './api'
vi.mock('@/components/markdown/Markdown', () => ({
  Markdown: ({ children }: { children: string }) => <div>{children}</div>,
}))
beforeEach(async () => {
  await i18n.changeLanguage('en')
  useClassroom.setState({ quizzes: {}, requesting: {} })
})
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})
const quiz = (id: string): QuizView => ({
  id,
  course_id: 'course-1',
  topic_id: 'topic-1',
  final: false,
  created_at: '',
  questions: [
    {
      id: `${id}-question`,
      kind: 'single',
      prompt: `Question for ${id}`,
      options: ['First', 'Second'],
    },
  ],
})
function mount(course = courseFixture, topicId: string | undefined = 'topic-1') {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={client}>
      <QuizSession course={course} topicId={topicId} onBack={() => {}} />
    </QueryClientProvider>,
  )
  return client
}
it('waits for quiz_ready, grades, receives feedback and requests a different quiz on retry', async () => {
  const attempts: Attempt[] = []
  const call = vi.spyOn(rpc, 'call').mockImplementation(async (method, params) => {
    if (method === 'classroom.request_quiz') return { requested: true } as never
    if (method === 'classroom.get_quiz')
      return quiz((params as { quiz_id: string }).quiz_id) as never
    if (method === 'classroom.attempts') return { attempts: structuredClone(attempts) } as never
    if (method === 'classroom.submit') {
      attempts.push({
        id: 'attempt-1',
        quiz_id: 'quiz-1',
        passed: false,
        results: [
          { question_id: 'quiz-1-question', correct: false, explanation: 'Review the lesson.' },
        ],
        created_at: courseFixture.created_at,
      })
      return { attempt: 'attempt-1', passed: false, results: attempts[0]!.results } as never
    }
    throw new Error(`Unexpected ${method}`)
  })
  const client = mount()
  await waitFor(() =>
    expect(call).toHaveBeenCalledWith('classroom.request_quiz', {
      course_id: 'course-1',
      topic_id: 'topic-1',
    }),
  )
  expect(await screen.findByText('Excalibur is preparing new questions…')).toBeInTheDocument()
  const ready = (quizId: string, topicId = 'topic-1') =>
    act(() =>
      applyClassroomEvent(client, {
        type: 'classroom.quiz_ready',
        payload: { course_id: 'course-1', topic_id: topicId, quiz_id: quizId },
      }),
    )
  ready('unrelated', 'topic-2')
  expect(screen.queryByRole('radio')).toBeNull()
  ready('quiz-1')
  fireEvent.click(await screen.findByRole('radio', { name: 'First' }))
  fireEvent.click(screen.getByRole('button', { name: 'Submit answers' }))
  expect(await screen.findByText('Excalibur is preparing your feedback…')).toBeInTheDocument()
  attempts[0]!.feedback = 'Review variables, then try again.'
  act(() =>
    applyClassroomEvent(client, {
      type: 'classroom.feedback_ready',
      payload: { course_id: 'course-1', topic_id: 'topic-1', attempt_id: 'attempt-1' },
    }),
  )
  expect((await screen.findAllByText('Review variables, then try again.')).length).toBeGreaterThan(
    0,
  )
  fireEvent.click(screen.getByRole('button', { name: 'Try with new questions' }))
  await waitFor(() =>
    expect(call.mock.calls.filter(([method]) => method === 'classroom.request_quiz')).toHaveLength(
      2,
    ),
  )
  expect(screen.queryByText('Question for quiz-1')).toBeNull()
  ready('quiz-2')
  expect(await screen.findByText('1. Question for quiz-2')).toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Submit answers' })).toBeDisabled()
})
it('uses the same flow for final exams and never requests a locked final', async () => {
  const call = vi
    .spyOn(rpc, 'call')
    .mockImplementation(async (method) =>
      method === 'classroom.attempts' ? ({ attempts: [] } as never) : ({} as never),
    )
  const lockedClient = new QueryClient()
  render(
    <QueryClientProvider client={lockedClient}>
      <QuizSession course={courseFixture} onBack={() => {}} />
    </QueryClientProvider>,
  )
  expect(screen.getByText('Complete every topic to unlock the exam.')).toBeInTheDocument()
  expect(call.mock.calls.filter(([method]) => method === 'classroom.request_quiz')).toHaveLength(0)
  cleanup()
  const course: Course = {
    ...courseFixture,
    units: courseFixture.units.map((unit) => ({
      ...unit,
      topics: unit.topics.map((topic) => ({ ...topic, status: 'completed' })),
    })),
    final_exam: { unlocked: true, passed: false },
  }
  render(
    <QueryClientProvider client={new QueryClient()}>
      <QuizSession course={course} onBack={() => {}} />
    </QueryClientProvider>,
  )
  await waitFor(() =>
    expect(call).toHaveBeenCalledWith('classroom.request_quiz', {
      course_id: 'course-1',
      final: true,
    }),
  )
})

it('reuses an unanswered quiz returned by request_quiz without a new ready event', async () => {
  vi.spyOn(rpc, 'call').mockImplementation(async (method) => {
    if (method === 'classroom.request_quiz')
      return { requested: false, quiz: quiz('existing') } as never
    if (method === 'classroom.get_quiz') return quiz('existing') as never
    if (method === 'classroom.attempts') return { attempts: [] } as never
    throw new Error(`Unexpected ${method}`)
  })
  mount()
  expect(await screen.findByText('1. Question for existing')).toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Submit answers' })).toBeDisabled()
})
