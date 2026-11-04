import type { TFunction } from 'i18next'
import type { BrowserActivityEvent } from './types'
export function activityLabel(activity: BrowserActivityEvent, t: TFunction): string {
  if (activity.kind === 'type_password') return t('browserPassword')
  if (activity.kind === 'navigate') {
    try {
      return t('browserNavigate', { host: new URL(activity.url ?? '').host })
    } catch {
      return t('browserNavigateGeneric')
    }
  }
  const targeted: Record<string, string> = {
    click: 'browserClick',
    type: 'browserType',
    select: 'browserSelect',
    hover: 'browserHover',
  }
  if (targeted[activity.kind])
    return t(targeted[activity.kind] + (activity.target ? '' : 'Generic'), {
      target: activity.target,
    })
  const labels: Record<string, string> = {
    read: 'browserRead',
    wait: 'browserWait',
    scroll: 'browserScroll',
    press: 'browserPress',
    upload: 'browserUpload',
    drag: 'browserDrag',
    download: 'browserDownload',
  }
  const key = labels[activity.kind]
  return key ? t(key) : ''
}
