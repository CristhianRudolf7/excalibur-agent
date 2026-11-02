import type { Reminder } from '@/lib/rpc/contract'

function dayNumber(date: Date, timezone: string) {
  const parts = new Intl.DateTimeFormat('en-CA', {
    timeZone: timezone,
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  }).formatToParts(date)
  const part = (type: string) => Number(parts.find((p) => p.type === type)?.value)
  return Date.UTC(part('year'), part('month') - 1, part('day')) / 86400000
}
export function reminderGroup(
  reminder: Reminder,
  now = new Date(),
  timezone = Intl.DateTimeFormat().resolvedOptions().timeZone,
) {
  const delta = dayNumber(new Date(reminder.due_at), timezone) - dayNumber(now, timezone)
  if (delta <= 0) return 'reminderToday'
  if (delta === 1) return 'reminderTomorrow'
  const weekday = new Date(dayNumber(now, timezone) * 86400000).getUTCDay()
  if (delta <= 7 - (weekday || 7)) return 'reminderWeek'
  return 'reminderLater'
}
export function formatReminderDate(date: string, locale: string, timezone: string) {
  try {
    return new Intl.DateTimeFormat(locale, {
      dateStyle: 'medium',
      timeStyle: 'short',
      timeZone: timezone,
    }).format(new Date(date))
  } catch {
    return date
  }
}
export function localInput(date: Date, timezone: string) {
  const parts = new Intl.DateTimeFormat('en-CA', {
    timeZone: timezone,
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hourCycle: 'h23',
  }).formatToParts(date)
  const part = (type: string) => parts.find((p) => p.type === type)?.value
  return `${part('year')}-${part('month')}-${part('day')}T${part('hour')}:${part('minute')}`
}
/** Interpret the picker in its selected IANA zone; reject nonexistent DST times. */
export function zonedInput(value: string, timezone: string) {
  if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$/.test(value)) throw new Error('Invalid date')
  const target = Date.parse(value + 'Z')
  let instant = target
  for (let i = 0; i < 4; i++) {
    const rendered = Date.parse(localInput(new Date(instant), timezone) + 'Z')
    instant += target - rendered
  }
  const result = new Date(instant)
  if (localInput(result, timezone) !== value) throw new Error('Invalid local time')
  return result.toISOString()
}
export function tomorrowAtNine(
  now = new Date(),
  timezone = Intl.DateTimeFormat().resolvedOptions().timeZone,
) {
  const day = new Date(dayNumber(now, timezone) * 86400000 + 86400000).toISOString().slice(0, 10)
  return zonedInput(`${day}T09:00`, timezone)
}
