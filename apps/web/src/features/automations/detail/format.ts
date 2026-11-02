import type { TFunction } from 'i18next'
import type { AutomationSchedule, AutomationStep } from '@/lib/rpc/contract'

export function formatDate(value: string, locale: string, timezone?: string) {
  return new Intl.DateTimeFormat(locale, {
    dateStyle: 'medium',
    timeStyle: 'short',
    timeZone: timezone,
  }).format(new Date(value))
}
export function formatSchedule(schedule: AutomationSchedule, t: TFunction, locale: string) {
  let text: string
  switch (schedule.kind) {
    case 'once':
      text = t('once', {
        date: schedule.at ? formatDate(schedule.at, locale, schedule.timezone) : '—',
      })
      break
    case 'interval':
      text = t('interval', { minutes: schedule.every_minutes })
      break
    case 'daily':
      text = t('daily', { time: schedule.time })
      break
    case 'weekly':
      text = t('weekly', {
        time: schedule.time,
        days: (schedule.weekdays ?? [])
          .map((day) =>
            new Intl.DateTimeFormat(locale, { weekday: 'long', timeZone: 'UTC' }).format(
              new Date(Date.UTC(2026, 10, 1 + day)),
            ),
          )
          .join(', '),
      })
      break
    case 'monthly':
      text = t('monthly', { day: schedule.day_of_month, time: schedule.time })
      break
    case 'cron':
      text = t('cron', { cron: schedule.cron })
      break
  }
  return `${text} · ${schedule.timezone}`
}
export function relativeTime(value: string, locale: string, now = Date.now()) {
  const seconds = (new Date(value).getTime() - now) / 1000
  const unit =
    Math.abs(seconds) < 60
      ? 'second'
      : Math.abs(seconds) < 3600
        ? 'minute'
        : Math.abs(seconds) < 86400
          ? 'hour'
          : 'day'
  const divisor = { second: 1, minute: 60, hour: 3600, day: 86400 }[unit]
  return new Intl.RelativeTimeFormat(locale, { numeric: 'auto' }).format(
    Math.round(seconds / divisor),
    unit,
  )
}

export function stepDetail(step: AutomationStep) {
  return step.instruction ?? step.tool ?? step.expression ?? step.message ?? step.code ?? ''
}
