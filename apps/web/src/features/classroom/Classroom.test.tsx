import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import i18n from '@/lib/i18n'
import { rpc } from '@/lib/rpc/client'
import { subscribeEvents } from '@/lib/rpc/events'
import { Classroom } from './Classroom'
import { useClassroom } from './store'
import { courseFixture, summaryFixture } from './fixtures'
vi.mock('@/features/chat/Chat', () => ({
  Chat: ({ context }: { context: unknown }) => (
    <div data-testid="conversation">{JSON.stringify(context)}</div>
  ),
}))
vi.mock('@/lib/rpc/events', () => ({ subscribeEvents: vi.fn(() => () => {}) }))
const client = () =>
  new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
function mount() {
  return render(
    <QueryClientProvider client={client()}>
      <Classroom />
    </QueryClientProvider>,
  )
}
beforeEach(async () => {
  await i18n.changeLanguage('en')
  vi.stubGlobal('matchMedia', () => ({
    matches: false,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  }))
  useClassroom.setState({
    courseId: undefined,
    topicId: undefined,
    preparing: {},
    requesting: {},
    quizzes: {},
  })
  Object.defineProperty(HTMLDialogElement.prototype, 'showModal', {
    configurable: true,
    value() {
      this.setAttribute('open', '')
    },
  })
  Object.defineProperty(HTMLDialogElement.prototype, 'close', {
    configurable: true,
    value() {
      this.removeAttribute('open')
    },
  })
})
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})
it('asks what to study and passes classroom context to the existing conversation', async () => {
  vi.spyOn(rpc, 'call').mockResolvedValue({ courses: [] } as never)
  mount()
  expect(await screen.findByText('What do you want to study?')).toBeInTheDocument()
  expect(screen.getByTestId('conversation')).toHaveTextContent('{"app":"classroom"}')
})
it('opens a newly created course from a live event and gives the header chat its context', async () => {
  vi.spyOn(rpc, 'call').mockImplementation(async (method) => {
    if (method === 'classroom.list') return { courses: [] } as never
    return courseFixture as never
  })
  mount()
  await screen.findByText('What do you want to study?')
  const connection = vi.mocked(subscribeEvents).mock.calls.at(-1)![0]
  connection.ready()
  connection.event({
    id: 'new',
    seq: 1,
    type: 'classroom.course_created',
    at: '',
    payload: { course_id: courseFixture.id },
  })
  expect(await screen.findByText(courseFixture.title)).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'Classroom conversation' }))
  expect(screen.getByTestId('conversation')).toHaveTextContent('"course_id":"course-1"')
})
it('lists course progress and activity and deletes only after confirmation', async () => {
  const call = vi.spyOn(rpc, 'call').mockImplementation(async (method) => {
    if (method === 'classroom.list') return { courses: [summaryFixture] } as never
    return {} as never
  })
  mount()
  expect(await screen.findByText(courseFixture.title)).toBeInTheDocument()
  expect(screen.getByRole('progressbar')).toHaveAttribute('value', '0')
  expect(screen.getByText(/Last activity:/)).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'Delete course' }))
  expect(call).not.toHaveBeenCalledWith('classroom.delete', expect.anything())
  fireEvent.click(screen.getAllByRole('button', { name: 'Delete course' }).at(-1)!)
  await waitFor(() =>
    expect(call).toHaveBeenCalledWith('classroom.delete', { course_id: 'course-1' }),
  )
})

it('opens an accessible mobile chat overlay and restores focus after Escape', async () => {
  vi.stubGlobal('matchMedia', () => ({
    matches: true,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  }))
  vi.spyOn(rpc, 'call').mockResolvedValue({ courses: [] } as never)
  const { container } = mount()
  await screen.findByText('What do you want to study?')
  const button = screen.getByRole('button', { name: 'Classroom conversation' })
  button.focus()
  fireEvent.click(button)
  expect(screen.getByRole('dialog', { name: 'Classroom conversation' })).toHaveAttribute(
    'aria-modal',
    'true',
  )
  expect(container.querySelector('.classroom-content')).toHaveAttribute('inert')
  fireEvent.keyDown(document, { key: 'Escape' })
  expect(screen.queryByRole('dialog')).toBeNull()
  expect(button).toHaveFocus()
})
