import type { Course, CourseSummary } from '@/lib/rpc/classroom'
/** Deterministic wire fixtures; no answer keys or generated lessons. */
export const courseFixture: Course = {
  id: 'course-1',
  title: 'Practical algebra',
  goal: 'Solve equations',
  level: 'Beginner',
  language: 'en',
  units: [
    {
      id: 'unit-1',
      title: 'Foundations',
      topics: [
        { id: 'topic-1', title: 'Variables', summary: 'Names and values', status: 'not_started' },
        { id: 'topic-2', title: 'Equations', summary: 'Finding unknowns', status: 'not_started' },
      ],
    },
  ],
  final_exam: { unlocked: false, passed: false },
  progress: 0,
  created_at: '2026-11-01T12:00:00Z',
  updated_at: '2026-11-03T12:00:00Z',
}
export const summaryFixture: CourseSummary = {
  id: courseFixture.id,
  title: courseFixture.title,
  progress: 0,
  total_topics: 2,
  completed_topics: 0,
  updated_at: courseFixture.updated_at,
}
