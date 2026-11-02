import { expect, it } from 'vitest'
import i18n from '@/lib/i18n'
import './i18n'
import { formatSchedule, relativeTime } from './format'
import type { AutomationSchedule } from '@/lib/rpc/contract'
it('describes all six schedules in English and Spanish with the configured timezone', () => {
  const schedules: AutomationSchedule[] = [
    { kind: 'once', timezone: 'America/Lima', at: '2026-11-03T14:00:00Z' },
    { kind: 'interval', timezone: 'UTC', every_minutes: 5 },
    { kind: 'daily', timezone: 'UTC', time: '09:00' },
    { kind: 'weekly', timezone: 'UTC', weekdays: [0, 1], time: '09:00' },
    { kind: 'monthly', timezone: 'UTC', day_of_month: 31, time: '09:00' },
    { kind: 'cron', timezone: 'UTC', cron: '0 9 * * 1' },
  ]
  const english = schedules.map((schedule) =>
    formatSchedule(schedule, i18n.getFixedT('en', 'automationDetail'), 'en'),
  )
  const spanish = schedules.map((schedule) =>
    formatSchedule(schedule, i18n.getFixedT('es', 'automationDetail'), 'es'),
  )
  expect(english[0]).toContain('9:00 AM')
  expect(english[1]).toBe('Every 5 minutes · UTC')
  expect(spanish[1]).toBe('Cada 5 minutos · UTC')
  expect(english[3]).toContain('Sunday, Monday')
  expect(spanish[3]).toContain('domingo, lunes')
  expect(english[4]).toContain('31')
  expect(spanish[5]).toContain('0 9 * * 1')
})
it('formats relative times in the active locale', () => {
  const now = Date.parse('2026-11-02T12:00:00Z')
  expect(relativeTime('2026-11-02T11:00:00Z', 'en', now)).toBe('1 hour ago')
  expect(relativeTime('2026-11-02T11:00:00Z', 'es', now)).toBe('hace 1 hora')
})
