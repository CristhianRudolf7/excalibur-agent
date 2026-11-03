// @vitest-environment node
import { readFileSync } from 'node:fs'
import { expect, expectTypeOf, it } from 'vitest'
import type { Attempt, ClassroomMethods, QuestionView } from './classroom'
import { classroomEventTypes } from './classroom'

it('uses the classroom methods and events exported by the backend', () => {
  const contract = readFileSync(
    new URL('../../../../../docs/development/classroom.md', import.meta.url),
    'utf8',
  )
  const methods: (keyof ClassroomMethods)[] = [
    'classroom.list',
    'classroom.get',
    'classroom.update_syllabus',
    'classroom.delete',
    'classroom.open_topic',
    'classroom.request_quiz',
    'classroom.get_quiz',
    'classroom.submit',
    'classroom.attempts',
  ]
  const exportedMethods = JSON.parse(
    readFileSync(
      new URL('../../../../../packages/contracts/methods.json', import.meta.url),
      'utf8',
    ),
  )
  const exportedEvents = JSON.parse(
    readFileSync(new URL('../../../../../packages/contracts/events.json', import.meta.url), 'utf8'),
  )
  expect(
    exportedMethods.methods
      .filter((entry: { name: string }) => entry.name.startsWith('classroom.'))
      .map((entry: { name: string }) => entry.name)
      .sort(),
  ).toEqual([...methods].sort())
  expect(
    exportedEvents.events
      .filter((entry: { type: string }) => entry.type.startsWith('classroom.'))
      .map((entry: { type: string }) => entry.type)
      .sort(),
  ).toEqual([...classroomEventTypes].sort())
  for (const name of [...methods, ...classroomEventTypes]) expect(contract).toContain(`\`${name}\``)
})
it('excludes answer keys and explanations from every question view', () => {
  type Single = Extract<QuestionView, { kind: 'single' | 'multiple' }>
  type Match = Extract<QuestionView, { kind: 'match' }>
  expectTypeOf<Extract<keyof Single, 'correct' | 'pairs' | 'explanation'>>().toEqualTypeOf<never>()
  expectTypeOf<Extract<keyof Match, 'correct' | 'pairs' | 'explanation'>>().toEqualTypeOf<never>()
})

it('keeps submitted answers out of browser attempt history', () => {
  expectTypeOf<Extract<keyof Attempt, 'answers'>>().toEqualTypeOf<never>()
})
