import type { Course } from '@/lib/rpc/classroom'
/** The final exam reserves the last percentage point even if all topics are complete. */
export function displayProgress(progress: number, passed = false) {
  return passed
    ? 100
    : Math.min(99, Math.max(0, Math.floor(Number.isFinite(progress) ? progress : 0)))
}
export function examUnlocked(course: Course) {
  const topics = course.units.flatMap((unit) => unit.topics)
  return (
    topics.length > 0 &&
    topics.every((topic) => topic.status === 'completed') &&
    course.final_exam.unlocked
  )
}

export type AnswerDraft = Record<string, number[]>
/** Validate completeness without grading or inspecting answer keys. */
export function buildAnswers(
  questions: import('@/lib/rpc/classroom').QuestionView[],
  draft: AnswerDraft,
): import('@/lib/rpc/classroom').Answer[] | undefined {
  if (questions.length === 0) return undefined
  const answers: import('@/lib/rpc/classroom').Answer[] = []
  for (const question of questions) {
    const values = draft[question.id] ?? []
    const count = question.kind === 'match' ? question.right.length : question.options.length
    if (
      !values.length ||
      values.some((value) => !Number.isInteger(value) || value < 0 || value >= count) ||
      new Set(values).size !== values.length
    )
      return undefined
    if (question.kind === 'single' && values.length !== 1) return undefined
    if (
      question.kind === 'match' &&
      (values.length !== question.left.length || question.left.length !== question.right.length)
    )
      return undefined
    answers.push(
      question.kind === 'match'
        ? { question_id: question.id, pairs: [...values] }
        : { question_id: question.id, selected: [...values].sort((a, b) => a - b) },
    )
  }
  return answers
}
export function toggleOption(selected: number[], index: number) {
  return selected.includes(index)
    ? selected.filter((value) => value !== index)
    : [...selected, index]
}

export function reorder<T>(items: T[], index: number, direction: number): T[] {
  const to = index + direction
  if (to < 0 || to >= items.length) return items
  const copy = [...items]
  ;[copy[index], copy[to]] = [copy[to]!, copy[index]!]
  return copy
}
