import { describe, expect, it } from 'vitest'
import { formatReminderDate, localInput, reminderGroup, tomorrowAtNine, zonedInput } from './format'
import type { Reminder } from '@/lib/rpc/contract'
const reminder = (due_at: string) => ({ due_at }) as Reminder
describe('reminder day groups', () => {
  const now = new Date('2026-11-02T12:00:00Z')
  it('groups overdue, today, tomorrow, this calendar week and later', () => {
    const dates = ['2026-11-01', '2026-11-02', '2026-11-03', '2026-11-08', '2026-11-09']
    expect(dates.map((date) => reminderGroup(reminder(date + 'T12:00:00Z'), now, 'UTC'))).toEqual([
      'reminderToday',
      'reminderToday',
      'reminderTomorrow',
      'reminderWeek',
      'reminderLater',
    ])
  })
  it('uses calendar days in the chosen zone around midnight and DST', () => {
    expect(reminderGroup(reminder('2026-11-03T02:00:00Z'), now, 'America/New_York')).toBe(
      'reminderToday',
    )
    expect(
      reminderGroup(
        reminder('2026-03-09T04:30:00Z'),
        new Date('2026-03-08T05:30:00Z'),
        'America/New_York',
      ),
    ).toBe('reminderTomorrow')
  })
})
describe('timezone scheduling and formatting', () => {
  it('round trips the picker in a non-local timezone and supports fractional offsets', () => {
    expect(zonedInput('2026-11-02T09:00', 'America/New_York')).toBe('2026-11-02T14:00:00.000Z')
    expect(zonedInput('2026-11-02T09:00', 'Asia/Kolkata')).toBe('2026-11-02T03:30:00.000Z')
    expect(localInput(new Date('2026-11-02T14:00:00Z'), 'America/New_York')).toBe(
      '2026-11-02T09:00',
    )
  })
  it('rejects invalid zones, dates and nonexistent DST times', () => {
    expect(() => zonedInput('2026-03-08T02:30', 'America/New_York')).toThrow()
    expect(() => zonedInput('2026-02-30T09:00', 'UTC')).toThrow()
    expect(() => zonedInput('2026-11-02T09:00', 'bad-zone')).toThrow()
    expect(() => zonedInput('', 'UTC')).toThrow()
  })
  it('snoozes to tomorrow at nine across a DST transition', () => {
    expect(tomorrowAtNine(new Date('2026-03-07T17:00:00Z'), 'America/New_York')).toBe(
      '2026-03-08T13:00:00.000Z',
    )
  })
  it('formats with locale and timezone and tolerates invalid server values', () => {
    expect(formatReminderDate('2026-11-02T14:00:00Z', 'en-US', 'America/New_York')).toContain(
      '9:00',
    )
    expect(formatReminderDate('bad', 'en', 'UTC')).toBe('bad')
  })
})
