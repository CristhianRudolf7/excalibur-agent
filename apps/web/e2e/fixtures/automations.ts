import type { Page } from '@playwright/test'
import type { Automation, AutomationRun } from '../../src/lib/rpc/contract'
export const sampleRun: AutomationRun = {
  id: 'run-1',
  automation_id: 'daily',
  version: 1,
  trigger: 'schedule',
  status: 'succeeded',
  started_at: '2026-11-02T08:00:00Z',
  finished_at: '2026-11-02T08:01:00Z',
  summary: 'Saved the news.',
  steps: [],
}
const daily: Automation = {
  id: 'daily',
  name: 'AI morning briefing',
  description: 'Summarize the latest AI news and save the highlights to a file.',
  status: 'active',
  version: 1,
  schedule: { kind: 'daily', time: '08:00', timezone: 'America/Lima' },
  steps: [
    { id: 's1', type: 'agent', title: 'Read news' },
    { id: 's2', type: 'condition', title: 'Any updates?' },
    { id: 's3', type: 'script', title: 'Save briefing' },
    { id: 's4', type: 'notify', title: 'Send summary' },
  ],
  edges: [
    { from: 's1', to: 's2' },
    { from: 's2', to: 's3', label: 'yes' },
    { from: 's2', to: 's4', label: 'no' },
    { from: 's3', to: 's4' },
  ],
  next_run_at: '2026-11-03T13:00:00Z',
  last_run: sampleRun,
  consecutive_failures: 0,
  created_at: '2026-11-01T00:00:00Z',
  updated_at: '2026-11-01T00:00:00Z',
}
export const sampleAutomations: Automation[] = [
  daily,
  {
    ...daily,
    id: 'paused',
    name: 'Project health check',
    status: 'paused',
    paused_reason: '3 consecutive failures',
    consecutive_failures: 3,
    next_run_at: undefined,
    last_run: { ...sampleRun, automation_id: 'paused', status: 'failed' },
  },
  {
    ...daily,
    id: 'weekly',
    name: 'Weekly review',
    schedule: { kind: 'weekly', weekdays: [1, 4], time: '09:30', timezone: 'UTC' },
    last_run: undefined,
  },
]
export async function mockAutomations(page: Page, initial = sampleAutomations, delay = 0) {
  let items = structuredClone(initial)
  const calls: { method: string; params: { id: string } }[] = []
  const { mockEvents } = await import('../event-fixture')
  const events = await mockEvents(page)
  await page.route('**/rpc', async (route) => {
    const req = route.request().postDataJSON()
    if (!req.method.startsWith('automations.')) return route.fallback()
    calls.push(req)
    let result: unknown
    const a = items.find((a) => a.id === req.params?.id)
    switch (req.method) {
      case 'automations.list':
        if (delay) await new Promise((r) => setTimeout(r, delay))
        result = { automations: items }
        break
      case 'automations.get':
        result = { automation: a, versions: [{ version: 1, created_at: daily.created_at }] }
        break
      case 'automations.runs':
        result = { runs: [sampleRun] }
        break
      case 'automations.run':
        result = {
          run: {
            ...sampleRun,
            id: 'manual',
            automation_id: a!.id,
            status: 'running',
            trigger: 'manual',
            started_at: new Date().toISOString(),
            finished_at: undefined,
          },
        }
        break
      case 'automations.pause':
        a!.status = 'paused'
        a!.paused_reason = 'user'
        result = a
        break
      case 'automations.resume':
        a!.status = 'active'
        a!.consecutive_failures = 0
        result = a
        break
      case 'automations.delete':
        items = items.filter((x) => x.id !== a!.id)
        result = { id: a!.id, deleted: true }
        break
    }
    await route.fulfill({ json: { jsonrpc: '2.0', id: req.id, result } })
  })
  return { calls, events }
}
