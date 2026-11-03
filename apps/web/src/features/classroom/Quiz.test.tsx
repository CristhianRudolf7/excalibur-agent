import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import i18n from '@/lib/i18n'
import { rpc } from '@/lib/rpc/client'
import type { QuizView } from '@/lib/rpc/classroom'
import { Quiz } from './Quiz'
import { buildAnswers, toggleOption } from './logic'
vi.mock('@/components/markdown/Markdown', () => ({
  Markdown: ({ children }: { children: string }) => <div>{children}</div>,
}))
export const quizFixture: QuizView = {
  id: 'quiz-1',
  course_id: 'course-1',
  topic_id: 'topic-1',
  final: false,
  created_at: '',
  questions: [
    { id: 'single', kind: 'single', prompt: 'Choose a variable', options: ['x', '3'] },
    {
      id: 'multiple',
      kind: 'multiple',
      prompt: 'Choose expressions',
      options: ['x + 1', 'y - 2', '='],
    },
    {
      id: 'match',
      kind: 'match',
      prompt: 'Pair terms',
      left: ['Variable', 'Constant'],
      right: ['5', 'y'],
    },
  ],
}
beforeEach(async () => {
  await i18n.changeLanguage('en')
})
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})
it('builds index-based answers for every kind and validates completeness without grading', () => {
  expect(buildAnswers(quizFixture.questions, {})).toBeUndefined()
  const draft = { single: [0], multiple: [1, 0], match: [1, 0] }
  expect(buildAnswers(quizFixture.questions, draft)).toEqual([
    { question_id: 'single', selected: [0] },
    { question_id: 'multiple', selected: [0, 1] },
    { question_id: 'match', pairs: [1, 0] },
  ])
  for (const invalid of [
    { ...draft, single: [0, 1] },
    { ...draft, single: [-1] },
    { ...draft, multiple: [3] },
    { ...draft, multiple: [0, 0] },
    { ...draft, match: [1, 1] },
    { ...draft, match: [1] },
    { ...draft, match: [1, NaN] },
  ])
    expect(buildAnswers(quizFixture.questions, invalid)).toBeUndefined()
  expect(buildAnswers([], {})).toBeUndefined()
  expect(toggleOption([0], 1)).toEqual([0, 1])
  expect(toggleOption([0, 1], 0)).toEqual([1])
})
it('renders accessible controls, submits every answer and shows only grading results and feedback', async () => {
  const onGraded = vi.fn(),
    onRetry = vi.fn()
  const grade = {
    attempt: 'attempt-1',
    passed: false,
    results: [
      { question_id: 'single', correct: true, explanation: 'Variables stand for values.' },
      { question_id: 'multiple', correct: false, explanation: 'Review expressions.' },
      { question_id: 'match', correct: true },
    ],
  }
  const call = vi.spyOn(rpc, 'call').mockResolvedValue(grade as never)
  // Extra wire fields must never reach the rendered question, even with an invalid server response.
  const quiz = {
    ...quizFixture,
    questions: quizFixture.questions.map((q) => ({
      ...q,
      correct: ['SECRET_KEY'],
      pairs: ['SECRET_PAIRS'],
      explanation: 'HIDDEN_EXPLANATION',
    })),
  }
  const client = new QueryClient()
  const view = render(
    <QueryClientProvider client={client}>
      <Quiz quiz={quiz} onGraded={onGraded} onRetry={onRetry} />
    </QueryClientProvider>,
  )
  expect(screen.getByRole('button', { name: 'Submit answers' })).toBeDisabled()
  expect(screen.queryByText(/SECRET|HIDDEN/)).toBeNull()
  fireEvent.click(screen.getByRole('radio', { name: 'x' }))
  fireEvent.click(screen.getByRole('checkbox', { name: 'x + 1' }))
  fireEvent.click(screen.getByRole('checkbox', { name: 'y - 2' }))
  fireEvent.change(screen.getByLabelText('Match for Variable'), { target: { value: '1' } })
  fireEvent.change(screen.getByLabelText('Match for Constant'), { target: { value: '0' } })
  fireEvent.click(screen.getByRole('button', { name: 'Submit answers' }))
  await waitFor(() => expect(onGraded.mock.calls[0]?.[0]).toEqual(grade))
  expect(call).toHaveBeenCalledWith('classroom.submit', {
    quiz_id: 'quiz-1',
    answers: [
      { question_id: 'single', selected: [0] },
      { question_id: 'multiple', selected: [0, 1] },
      { question_id: 'match', pairs: [1, 0] },
    ],
  })
  expect(await screen.findByText('Incorrect')).toBeInTheDocument()
  expect(screen.getByText('Review expressions.')).toBeInTheDocument()
  expect(screen.getByRole('radio', { name: 'x' })).toBeDisabled()
  const attempt = {
    id: 'attempt-1',
    quiz_id: 'quiz-1',
    passed: false,
    results: grade.results,
    feedback: 'Practice combining symbols.',
    created_at: '',
  }
  view.rerender(
    <QueryClientProvider client={client}>
      <Quiz quiz={quiz} attempt={attempt} onGraded={onGraded} onRetry={onRetry} />
    </QueryClientProvider>,
  )
  expect(screen.getByText('Practice combining symbols.')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'Try with new questions' }))
  expect(onRetry).toHaveBeenCalledOnce()
  expect(view.container.textContent).not.toMatch(/SECRET|HIDDEN/)
})
