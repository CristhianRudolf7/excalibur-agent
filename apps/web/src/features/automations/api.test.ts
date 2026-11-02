import { expect, it } from 'vitest'
import { QueryClient } from '@tanstack/react-query'
import i18n from '@/lib/i18n'
import { describeSchedule, relativeTime, applyAutomationEvent } from './api'
import { automationPrefix, visibleAutomationMessage } from './context'
import { sampleAutomations, sampleRun } from '../../../e2e/fixtures/automations'

it('describes every schedule in Spanish and English with the schedule timezone', () => {
  const es = i18n.getFixedT('es'),
    en = i18n.getFixedT('en')
  expect(describeSchedule({ kind: 'daily', timezone: 'UTC', time: '08:00' }, es, 'es')).toBe(
    'Todos los días a las 08:00',
  )
  expect(
    describeSchedule(
      { kind: 'weekly', timezone: 'UTC', weekdays: [1, 4], time: '09:30' },
      es,
      'es',
    ),
  ).toBe('Cada lunes y jueves a las 09:30')
  expect(
    describeSchedule({ kind: 'interval', timezone: 'UTC', every_minutes: 120 }, en, 'en'),
  ).toBe('Every 2 hours')
  expect(describeSchedule({ kind: 'interval', timezone: 'UTC', every_minutes: 5 }, es, 'es')).toBe(
    'Cada 5 minutos',
  )
  expect(
    describeSchedule(
      { kind: 'monthly', timezone: 'UTC', day_of_month: 1, time: '07:00' },
      es,
      'es',
    ),
  ).toBe('El día 1 de cada mes a las 07:00')
  expect(describeSchedule({ kind: 'cron', timezone: 'UTC', cron: '0 */3 * * *' }, en, 'en')).toBe(
    'Cron: 0 */3 * * *',
  )
  expect(
    describeSchedule(
      { kind: 'once', timezone: 'America/Lima', at: '2026-11-05T15:00:00Z' },
      en,
      'en',
    ),
  ).toContain('10:00')
})
it('formats future and past times with deterministic clocks', () => {
  const now = Date.parse('2026-11-02T12:00:00Z')
  expect(relativeTime('2026-11-02T15:00:00Z', 'es', now)).toBe('dentro de 3 h')
  expect(relativeTime('2026-11-02T11:55:00Z', 'es', now)).toBe('hace 5 min')
  expect(relativeTime('2026-11-04T12:00:00Z', 'en', now)).toBe('in 2 days')
})
it('updates both automation caches, upserts runs and removes deleted queries', () => {
  const client = new QueryClient()
  const a = sampleAutomations[0]!
  client.setQueryData(['automations'], { automations: sampleAutomations })
  client.setQueryData(['automation', a.id], { automation: a, versions: [] })
  const run = { ...sampleRun, status: 'running' as const }
  applyAutomationEvent(client, { type: 'automation.run', payload: { run } })
  applyAutomationEvent(client, {
    type: 'automation.run',
    payload: { run: { ...run, status: 'succeeded' } },
  })
  expect(client.getQueryData(['automation-runs', a.id])).toEqual({ runs: [sampleRun] })
  expect(
    client.getQueryData<{ automations: typeof sampleAutomations }>(['automations'])?.automations[0]
      ?.last_run?.status,
  ).toBe('succeeded')
  applyAutomationEvent(client, {
    type: 'automation.updated',
    payload: { automation: { ...a, status: 'paused' } },
  })
  expect(
    client.getQueryData<{ automation: typeof a }>(['automation', a.id])?.automation.status,
  ).toBe('paused')
  applyAutomationEvent(client, { type: 'automation.deleted', payload: { id: a.id } })
  expect(client.getQueryData(['automation', a.id])).toBeUndefined()
  expect(
    client.getQueryData<{ automations: typeof sampleAutomations }>(['automations'])?.automations,
  ).toHaveLength(2)
})
it('prefixes context in both languages while keeping the visible bubble compact', () => {
  const a = sampleAutomations[0]!
  const text = automationPrefix('Change it', a, i18n.getFixedT('es'), 'es')
  expect(text).toContain(
    '[Automatización adjunta: id=daily versión=1 nombre=«AI morning briefing»]',
  )
  expect(text).toContain('s2 · condition · Any updates?')
  expect(visibleAutomationMessage(text, i18n.getFixedT('es'))).toBe(
    '📎 Automatización: AI morning briefing\n\nChange it',
  )
  expect(automationPrefix('Hi', undefined, i18n.getFixedT('en'), 'en')).toBe('Hi')
  expect(automationPrefix('Change', a, i18n.getFixedT('en'), 'en')).toContain(
    '[Attached automation: id=daily version=1 name=«AI morning briefing»]',
  )
})
