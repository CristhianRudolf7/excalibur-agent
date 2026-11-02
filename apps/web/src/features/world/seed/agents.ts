import type { WorldAgent } from '../events'
export const agents: WorldAgent[] = [
  {
    id: 'excalibur',
    name: 'Excalibur',
    homeRoomId: 'voice',
    sprite: { variant: 'hood', tone: 'var(--primary)' },
  },
]
export const subagents: WorldAgent[] = [
  {
    id: 'source-checker',
    name: 'worldHelperVerifier',
    parentId: 'excalibur',
    homeRoomId: 'library',
    sprite: { variant: 'hood', tone: 'var(--violet)', scale: 0.65 },
  },
  {
    id: 'test-runner',
    name: 'worldHelperTests',
    parentId: 'excalibur',
    homeRoomId: 'code',
    sprite: { variant: 'cap', tone: 'var(--blue)', scale: 0.65 },
  },
  {
    id: 'chart-helper',
    name: 'worldHelperCharts',
    parentId: 'excalibur',
    homeRoomId: 'finance',
    sprite: { variant: 'beard', tone: 'var(--green)', scale: 0.65 },
  },
  {
    id: 'layout-helper',
    name: 'worldHelperLayout',
    parentId: 'excalibur',
    homeRoomId: 'launch',
    sprite: { variant: 'bow', tone: 'var(--pink)', scale: 0.65 },
  },
]
