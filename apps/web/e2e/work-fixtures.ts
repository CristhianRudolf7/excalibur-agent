import type { Page } from '@playwright/test'
import { mockEvents } from './event-fixture'
import type { Reminder, TaskSummary } from '../src/lib/rpc/contract'
const timestamp = '2026-11-02T12:00:00Z'
export async function mockWork(page: Page) {
  const tasks: TaskSummary[] = [
    {
      id: 'task-1',
      short_code: 'EXC-201',
      title: 'Ship the compiler',
      status: 'in_progress',
      level: 'project',
      project_id: 'project-1',
      created_at: timestamp,
      updated_at: timestamp,
      criteria_met: 1,
      criteria_total: 2,
    },
    {
      id: 'task-2',
      short_code: 'EXC-202',
      title: 'Review the release',
      status: 'pending',
      level: 'brief',
      created_at: timestamp,
      updated_at: timestamp,
      criteria_met: 0,
      criteria_total: 1,
    },
  ]
  const reminders: Reminder[] = [
    {
      id: 'reminder-1',
      text: 'Review release notes',
      due_at: new Date().toISOString(),
      timezone: 'UTC',
      status: 'scheduled',
      created_at: timestamp,
    },
  ]
  const projects = [
    {
      id: 'project-1',
      name: 'Compiler',
      description: 'Build tools',
      roots: ['/workspace/compiler'],
      status: 'active',
      created_at: timestamp,
      updated_at: timestamp,
    },
  ]
  const events = await mockEvents(page)
  const state = {
    tasks,
    reminders,
    projects,
    failMutation: false,
    unavailable: new Set<string>(),
    calls: [] as { method: string; params: Record<string, unknown> }[],
    emit: events.emit,
  }
  await page.route('**/rpc', async (route) => {
    const {
      id,
      method,
      params = {},
    } = route.request().postDataJSON() as {
      id: string
      method: string
      params?: Record<string, unknown>
    }
    if (method.startsWith('agents.')) return route.fallback()
    state.calls.push({ method, params })
    const error = async () =>
      route.fulfill({
        json: { jsonrpc: '2.0', id, error: { code: -32601, message: 'Unavailable' } },
      })
    if (state.unavailable.has(method)) return error()
    let result: unknown
    if (method === 'activity.list') result = { items: [] }
    else if (method === 'principal.get')
      result = { id: 'local', name: 'Test User', timezone: 'UTC', language: 'es' }
    else if (method === 'tasks.query') {
      const filtered = state.tasks.filter(
        (task) =>
          (!params.text || task.title.toLowerCase().includes(String(params.text).toLowerCase())) &&
          (!params.status || (params.status as string[]).includes(task.status)) &&
          (!params.project_id || params.project_id === task.project_id),
      )
      const counts = Object.fromEntries(
        ['pending', 'in_progress', 'blocked', 'done', 'failed', 'cancelled'].map((s) => [
          s,
          state.tasks.filter((task) => task.status === s).length,
        ]),
      )
      result = {
        tasks: params.cursor ? filtered.slice(1) : filtered.slice(0, 1),
        next_cursor: !params.cursor && filtered.length > 1 ? 'next' : '',
        counts,
      }
    } else if (method === 'tasks.get')
      result = {
        ...state.tasks.find((task) => task.id === params.id),
        request: 'Deliver a working compiler.',
        requirements: [{ id: 'req-1', text: 'Compile source files' }],
        criteria: [{ id: 'criterion-1', text: 'Tests pass', state: 'met', method: 'go test' }],
        evidence: [
          {
            criterion_id: 'criterion-1',
            kind: 'test',
            summary: 'All tests green',
            at: timestamp,
            ok: true,
          },
        ],
        plan: [
          { id: 'step-1', title: 'Implement parser', status: 'done', depends_on: [] },
          { id: 'step-2', title: 'Compile', status: 'in_progress', depends_on: ['step-1'] },
        ],
        notes: 'Ready for review',
        runs: [{ id: 'run-1', state: 'running', started_at: timestamp }],
      }
    else if (method === 'project.list') result = { projects: state.projects }
    else if (method === 'project.get')
      result = state.projects.find((p) => p.id === params.project_id)
    else if (method === 'project.register') {
      if (state.failMutation) return error()
      const project = {
        ...projects[0]!,
        id: 'project-2',
        name: String(params.name),
        roots: params.roots as string[],
      }
      state.projects.push(project)
      result = project
    } else if (method === 'files.list')
      result = { entries: params.path ? ['src/main.go'] : ['README.md', 'src/'], next_cursor: '' }
    else if (method === 'reminders.list') result = { reminders: state.reminders }
    else if (method === 'reminders.create') {
      if (state.failMutation) return error()
      const reminder = {
        id: 'reminder-2',
        text: String(params.text),
        due_at: String(params.due_at),
        timezone: String(params.timezone),
        repeat: String(params.repeat),
        status: 'scheduled',
        created_at: timestamp,
      } as Reminder
      state.reminders.push(reminder)
      result = reminder
    } else if (method === 'reminders.update') {
      if (state.failMutation) return error()
      const reminder = state.reminders.find((r) => r.id === params.id)!
      Object.assign(reminder, params, {
        due_at: params.snooze_until ?? params.due_at ?? reminder.due_at,
      })
      result = reminder
    } else if (method === 'reminders.delete') {
      if (state.failMutation) return error()
      state.reminders = state.reminders.filter((r) => r.id !== params.id)
      result = { id: params.id, deleted: true }
    } else if (method === 'conversation.history') result = { messages: [] }
    else return error()
    await route.fulfill({ json: { jsonrpc: '2.0', id, result } })
  })
  return state
}
