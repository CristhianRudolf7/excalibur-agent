import type { Automation, AutomationRun, AutomationStep } from '@/lib/rpc/contract'
export const sampleSteps: AutomationStep[] = [
  {
    id: 's1',
    type: 'agent',
    title: 'Review the project',
    instruction: 'Read the latest project notes and identify the most important changes.',
  },
  {
    id: 's2',
    type: 'condition',
    title: 'Changes found?',
    expression: 'steps.s1.output.changed === true',
  },
  {
    id: 's3',
    type: 'script',
    title: 'Build the digest',
    language: 'python',
    code: 'import json\nprint(json.dumps({"digest": "Ready"}))',
  },
  {
    id: 's4',
    type: 'tool',
    title: 'Read project status',
    tool: 'project.status',
    args: { project: 'excalibur', include: ['tasks'] },
  },
  {
    id: 's5',
    type: 'notify',
    title: 'Share the summary',
    message: 'Your project digest: {{steps.s3.output}}',
  },
]
export const sampleAutomation: Automation = {
  id: 'daily-digest',
  name: 'Daily project digest',
  description: 'A clear view of what changed, ready before your first coffee.',
  status: 'active',
  version: 2,
  schedule: { kind: 'daily', timezone: 'America/Lima', time: '09:00' },
  steps: sampleSteps,
  edges: [
    { from: 's1', to: 's2' },
    { from: 's2', to: 's3', label: 'yes' },
    { from: 's2', to: 's4', label: 'no' },
    { from: 's3', to: 's5' },
    { from: 's4', to: 's5' },
  ],
  next_run_at: '2026-11-03T14:00:00Z',
  consecutive_failures: 0,
  created_at: '2026-11-01T14:00:00Z',
  updated_at: '2026-11-02T14:00:00Z',
}
export const sampleRun: AutomationRun = {
  id: 'run-1',
  automation_id: sampleAutomation.id,
  version: 2,
  trigger: 'schedule',
  status: 'succeeded',
  started_at: '2026-11-02T14:00:00Z',
  finished_at: '2026-11-02T14:00:12Z',
  summary: 'The project digest is ready.',
  steps: sampleSteps.map((step) => ({
    step_id: step.id,
    status: step.id === 's4' ? 'skipped' : 'succeeded',
    output_preview: step.id === 's1' ? 'Three important changes found.' : undefined,
  })),
}
export const sampleVersions = [
  { version: 2, created_at: '2026-11-02T14:00:00Z', note: 'Added project status fallback.' },
  { version: 1, created_at: '2026-11-01T14:00:00Z', note: 'First approved version.' },
]
export const linearAutomation: Automation = {
  ...sampleAutomation,
  steps: [sampleSteps[0]!, sampleSteps[2]!, sampleSteps[4]!],
  edges: [
    { from: 's1', to: 's3' },
    { from: 's3', to: 's5' },
  ],
}
