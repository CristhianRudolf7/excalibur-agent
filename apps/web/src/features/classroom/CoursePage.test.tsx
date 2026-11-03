import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import i18n from '@/lib/i18n'
import { rpc } from '@/lib/rpc/client'
import { CoursePage } from './CoursePage'
import { courseFixture } from './fixtures'
import { displayProgress, examUnlocked } from './logic'
import { reorder } from './logic'
const completeCourse = () => ({
  ...courseFixture,
  progress: 99,
  units: courseFixture.units.map((unit) => ({
    ...unit,
    topics: unit.topics.map((topic) => ({ ...topic, status: 'completed' as const })),
  })),
  final_exam: { unlocked: true, passed: false },
})
function mount(course = structuredClone(courseFixture)) {
  const props = { course, onTopic: vi.fn(), onExam: vi.fn() }
  render(
    <QueryClientProvider client={new QueryClient()}>
      <CoursePage {...props} />
    </QueryClientProvider>,
  )
  return props
}
beforeEach(async () => {
  await i18n.changeLanguage('en')
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
})
it('reserves 100% for the passed final and rejects empty or partially complete syllabuses', () => {
  expect(displayProgress(100)).toBe(99)
  expect(displayProgress(12, true)).toBe(100)
  expect(displayProgress(-5)).toBe(0)
  expect(displayProgress(NaN)).toBe(0)
  expect(examUnlocked(courseFixture)).toBe(false)
  expect(examUnlocked({ ...completeCourse(), units: [] })).toBe(false)
  expect(examUnlocked(completeCourse())).toBe(true)
})
it('allows opening the second topic before the first and locks the final', () => {
  const props = mount()
  fireEvent.click(screen.getByRole('button', { name: /Equations/ }))
  expect(props.onTopic).toHaveBeenCalledWith('topic-2')
  expect(screen.getAllByText('Not started')).toHaveLength(2)
  expect(screen.getByRole('button', { name: 'Start exam' })).toBeDisabled()
})
it('unlocks the final at 99% and shows the passed state at 100%', () => {
  const props = mount(completeCourse())
  expect(screen.getByRole('progressbar')).toHaveAttribute('value', '99')
  fireEvent.click(screen.getByRole('button', { name: 'Start exam' }))
  expect(props.onExam).toHaveBeenCalledOnce()
  cleanup()
  mount({ ...completeCourse(), final_exam: { unlocked: true, passed: true } })
  expect(screen.getByRole('progressbar')).toHaveAttribute('value', '100')
  expect(screen.queryByRole('button', { name: 'Start exam' })).toBeNull()
})
it('saves renamed, reordered and newly added topics with existing IDs intact', async () => {
  const call = vi.spyOn(rpc, 'call').mockResolvedValue(courseFixture as never)
  mount()
  fireEvent.click(screen.getByRole('button', { name: 'Edit syllabus' }))
  fireEvent.change(screen.getAllByLabelText('Topic title')[0]!, { target: { value: 'Symbols' } })
  fireEvent.click(screen.getByRole('button', { name: 'Move Symbols down' }))
  fireEvent.click(screen.getByRole('button', { name: 'Add topic' }))
  fireEvent.click(screen.getByRole('button', { name: 'Add unit' }))
  fireEvent.click(screen.getByRole('button', { name: 'Save syllabus' }))
  await waitFor(() =>
    expect(call).toHaveBeenCalledWith('classroom.update_syllabus', {
      course_id: 'course-1',
      units: [
        expect.objectContaining({
          id: 'unit-1',
          topics: [
            expect.objectContaining({ id: 'topic-2' }),
            expect.objectContaining({ id: 'topic-1', title: 'Symbols' }),
            { title: 'New topic', summary: '' },
          ],
        }),
        expect.objectContaining({ title: 'New unit', topics: [] }),
      ],
    }),
  )
})
it('confirms removal of completed topics and whole units before altering the draft', () => {
  mount(completeCourse())
  fireEvent.click(screen.getByRole('button', { name: 'Edit syllabus' }))
  fireEvent.click(screen.getAllByRole('button', { name: 'Remove topic' })[0]!)
  let dialog = screen.getByRole('dialog')
  fireEvent.click(within(dialog).getAllByRole('button', { name: 'Close' })[0]!)
  expect(screen.getAllByLabelText('Topic title')).toHaveLength(2)
  fireEvent.click(screen.getByRole('button', { name: 'Remove unit' }))
  dialog = screen.getByRole('dialog')
  fireEvent.click(within(dialog).getByRole('button', { name: 'Confirm removal' }))
  expect(screen.queryByLabelText('Topic title')).toBeNull()
  expect(screen.getByRole('button', { name: 'Save syllabus' })).toBeEnabled()
})
it('keeps order unchanged at the boundaries', () => {
  expect(reorder(['a', 'b'], 0, -1)).toEqual(['a', 'b'])
  expect(reorder(['a', 'b'], 1, 1)).toEqual(['a', 'b'])
})

it('renames and reorders units, moves a topic between them and removes unfinished content directly', async () => {
  const course = structuredClone(courseFixture)
  course.units.push({
    id: 'unit-2',
    title: 'Other',
    topics: [{ id: 'topic-3', title: 'Practice', summary: '', status: 'not_started' }],
  })
  const call = vi.spyOn(rpc, 'call').mockResolvedValue(course as never)
  mount(course)
  fireEvent.click(screen.getByRole('button', { name: 'Edit syllabus' }))
  fireEvent.change(screen.getAllByLabelText('Unit title')[1]!, { target: { value: 'Exercises' } })
  fireEvent.click(screen.getByRole('button', { name: 'Move Exercises up' }))
  fireEvent.change(screen.getAllByLabelText('Move to unit')[1]!, { target: { value: 'unit-2' } })
  fireEvent.click(screen.getAllByRole('button', { name: 'Remove topic' })[0]!)
  expect(screen.queryByRole('dialog')).toBeNull()
  fireEvent.click(screen.getAllByRole('button', { name: 'Remove unit' })[1]!)
  fireEvent.click(screen.getByRole('button', { name: 'Save syllabus' }))
  await waitFor(() =>
    expect(call).toHaveBeenCalledWith('classroom.update_syllabus', {
      course_id: course.id,
      units: [
        {
          id: 'unit-2',
          title: 'Exercises',
          topics: [
            { id: 'topic-1', title: 'Variables', summary: course.units[0]!.topics[0]!.summary },
          ],
        },
      ],
    }),
  )
})
it('keeps the syllabus draft available after a failed save', async () => {
  vi.spyOn(rpc, 'call').mockRejectedValue(new Error('offline'))
  mount()
  fireEvent.click(screen.getByRole('button', { name: 'Edit syllabus' }))
  fireEvent.change(screen.getAllByLabelText('Topic title')[0]!, { target: { value: 'Symbols' } })
  fireEvent.click(screen.getByRole('button', { name: 'Save syllabus' }))
  expect(await screen.findByRole('alert')).toBeInTheDocument()
  expect(screen.getAllByLabelText('Topic title')[0]).toHaveValue('Symbols')
  expect(screen.getByRole('button', { name: 'Save syllabus' })).toBeEnabled()
})
