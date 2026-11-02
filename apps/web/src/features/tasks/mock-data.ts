import type { Status } from '@/components/shared'
import type { AgentId, Color } from '@/features/shell/mock-data'
export interface Task {
  id: string
  title: string
  label: string
  color: Color
  assignee: AgentId
  status: Status
  date: string
}
export const getTasks = (): Task[] => [
  {
    id: 'EXC-142',
    title: 'researchVoice',
    label: 'research',
    color: 'blue',
    assignee: 'excalibur',
    status: 'progress',
    date: 'today',
  },
  {
    id: 'EXC-143',
    title: 'testLatency',
    label: 'tests',
    color: 'green',
    assignee: 'excalibur',
    status: 'progress',
    date: 'today',
  },
  {
    id: 'EXC-144',
    title: 'verifySources',
    label: 'quality',
    color: 'pink',
    assignee: 'excalibur',
    status: 'pending',
    date: 'tomorrow',
  },
  {
    id: 'EXC-145',
    title: 'writeComparison',
    label: 'document',
    color: 'violet',
    assignee: 'excalibur',
    status: 'pending',
    date: 'tomorrow',
  },
  {
    id: 'EXC-139',
    title: 'criteria',
    label: 'analysis',
    color: 'blue',
    assignee: 'excalibur',
    status: 'done',
    date: 'october',
  },
  {
    id: 'EXC-140',
    title: 'providers',
    label: 'research',
    color: 'blue',
    assignee: 'excalibur',
    status: 'done',
    date: 'october',
  },
]
