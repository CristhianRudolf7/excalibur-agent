import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import i18n from '@/lib/i18n'
import { rpc } from '@/lib/rpc/client'
import { sendMessage } from '@/features/chat/live'
import type { Course } from '@/lib/rpc/classroom'
import { TopicPage } from './TopicPage'
import { courseFixture } from './fixtures'
import { receiveClassroomEvent, useClassroom } from './store'
vi.mock('@/components/markdown/Markdown', () => ({
  Markdown: ({ children }: { children: string }) => <div>{children}</div>,
}))
vi.mock('@/features/whiteboard/Whiteboard', () => ({
  Whiteboard: ({ boardId }: { boardId: string }) => <div data-testid="board">{boardId}</div>,
}))
vi.mock('@/features/chat/live', () => ({
  conversationId: () => 'cnv_web_excalibur',
  sendMessage: vi.fn(async () => true),
}))
function mount(course: Course) {
  const props = { course, topicId: 'topic-2', onBack: vi.fn(), onQuiz: vi.fn(), onChat: vi.fn() }
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const view = render(
    <QueryClientProvider client={client}>
      <TopicPage {...props} />
    </QueryClientProvider>,
  )
  return { ...view, props, client }
}
beforeEach(async () => {
  await i18n.changeLanguage('en')
  useClassroom.setState({ preparing: {}, requesting: {}, quizzes: {} })
})
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})
it('opens the requested topic through RPC and waits for its lesson', async () => {
  const call = vi
    .spyOn(rpc, 'call')
    .mockResolvedValue({ topic: courseFixture.units[0]!.topics[1]! } as never)
  mount(courseFixture)
  expect(await screen.findByText('Excalibur is preparing the class…')).toBeInTheDocument()
  await waitFor(() =>
    expect(call).toHaveBeenCalledWith('classroom.open_topic', {
      course_id: 'course-1',
      topic_id: 'topic-2',
    }),
  )
  receiveClassroomEvent('classroom.class_requested', { course_id: 'course-1', topic_id: 'topic-2' })
  expect(useClassroom.getState().preparing['course-1:topic-2']).toBe(true)
  receiveClassroomEvent('classroom.class_ready', { course_id: 'course-1', topic_id: 'topic-2' })
  expect(useClassroom.getState().preparing['course-1:topic-2']).toBe(false)
})
it('renders a stored whiteboard and markdown and requests regeneration through the same conversation', async () => {
  const course = structuredClone(courseFixture)
  course.units[0]!.topics[1]!.class = {
    format: 'whiteboard',
    content: 'An equation balances two expressions.',
    whiteboard_id: 'board-42',
    created_at: '',
  }
  vi.spyOn(rpc, 'call').mockResolvedValue({ topic: course.units[0]!.topics[1]! } as never)
  const { props } = mount(course)
  expect(screen.getByText('An equation balances two expressions.')).toBeInTheDocument()
  expect(screen.getByTestId('board')).toHaveTextContent('board-42')
  fireEvent.click(screen.getByRole('button', { name: 'Start quiz' }))
  expect(props.onQuiz).toHaveBeenCalledOnce()
  fireEvent.click(screen.getByRole('button', { name: 'Request a new class' }))
  await waitFor(() =>
    expect(sendMessage).toHaveBeenCalledWith(
      'excalibur',
      expect.stringContaining('Equations'),
      [],
      undefined,
      undefined,
      { app: 'classroom', course_id: 'course-1', topic_id: 'topic-2' },
    ),
  )
  expect(props.onChat).toHaveBeenCalledOnce()
})
it('does not retry an open failure automatically and allows retrying manually', async () => {
  const call = vi.spyOn(rpc, 'call').mockRejectedValueOnce(new Error('offline'))
  mount(courseFixture)
  expect(await screen.findByRole('alert')).toBeInTheDocument()
  call.mockResolvedValueOnce({ topic: courseFixture.units[0]!.topics[1]! } as never)
  fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
  await waitFor(() => expect(call).toHaveBeenCalledTimes(2))
})
