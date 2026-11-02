import { expect, it } from 'vitest'
import { filterSkills, groupSkills } from './catalog'
import type { SkillSummary, SkillGroup } from '@/lib/rpc/contract'
const skills: SkillSummary[] = [
  {
    name: 'review',
    description: 'Read code',
    group: 'code',
    version: '1',
    source: 'builtin',
    state: 'enabled',
    editable: false,
    deletable: false,
    tools: [],
  },
  {
    name: 'notes',
    description: 'Write notes',
    group: 'writing',
    version: '1',
    source: 'user',
    state: 'disabled',
    editable: true,
    deletable: true,
    tools: [],
  },
  {
    name: 'waiting',
    description: 'Await install',
    group: 'unknown',
    version: '1',
    source: 'project',
    state: 'pending',
    editable: true,
    deletable: true,
    tools: [],
  },
]
it('combines case-insensitive text, source and lifecycle filters', () => {
  expect(filterSkills(skills, { text: ' READ ', source: 'builtin', state: 'enabled' })).toEqual([
    skills[0],
  ])
  expect(filterSkills(skills, { text: '', source: 'user', state: 'enabled' })).toEqual([])
  expect(filterSkills(skills, { text: 'notes', source: '', state: 'disabled' })).toEqual([
    skills[1],
  ])
  expect(filterSkills(skills, { text: '', source: '', state: 'other' })).toEqual([skills[2]])
})
it('preserves catalog order, omits empty groups and includes unknown groups', () => {
  const groups = ['writing', 'empty', 'code'].map((id) => ({
    id,
    name_en: id,
    name_es: id,
    description_en: '',
    description_es: '',
    count: 1,
  })) satisfies SkillGroup[]
  expect(groupSkills(skills, groups).map((g) => [g.id, g.skills.length])).toEqual([
    ['writing', 1],
    ['code', 1],
    ['unknown', 1],
  ])
  expect(groupSkills([], groups)).toEqual([])
})
