import { useEffect, useMemo, useState } from 'react'
import { Popover } from 'radix-ui'
import { Check, ChevronDown, Search } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useProfile } from '@/features/profile/api'
import { Feedback } from './feedback'
import { browserTimeZone, localClock, timeZones } from './time-zone'
import { useUpdateTimeZone } from './time-zone-api'

export function TimeZone() {
  const { t, i18n } = useTranslation()
  const profile = useProfile()
  const update = useUpdateTimeZone()
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState('')
  const [now, setNow] = useState(() => new Date())
  useEffect(() => {
    const timer = window.setInterval(() => setNow(new Date()), 1000)
    return () => window.clearInterval(timer)
  }, [])
  const zone = profile.data?.timezone ?? 'UTC'
  const zones = useMemo(() => timeZones(zone), [zone])
  const matches = zones.filter((name) =>
    name.toLowerCase().replaceAll('_', ' ').includes(search.toLowerCase().replaceAll('_', ' ')),
  )
  const automatic = profile.data?.timezone_source !== 'manual'
  return (
    <div className="settings-time-zone">
      <h3>{t('settingsTimeZone')}</h3>
      <p>{t('settingsTimeZoneNote')}</p>
      <Feedback query={profile} />
      {profile.data && (
        <>
          <div className="settings-time-zone-switch">
            <button
              type="button"
              role="switch"
              className="settings-switch"
              aria-label={t('settingsTimeZoneAuto')}
              aria-checked={automatic}
              aria-disabled={update.isPending}
              onClick={() => {
                if (update.isPending) return
                const enabled = !automatic
                update.mutate({
                  timezone: enabled ? browserTimeZone() : zone,
                  timezone_source: enabled ? 'auto' : 'manual',
                  ...(enabled ? { enable_auto: true } : {}),
                })
              }}
            >
              <span />
            </button>
            <span>{t('settingsTimeZoneAuto')}</span>
          </div>
          <Popover.Root
            open={open}
            onOpenChange={(next) => {
              if (next && update.isPending) return
              setOpen(next)
              if (!next) setSearch('')
            }}
          >
            <Popover.Trigger
              className="settings-filter-trigger settings-time-zone-trigger"
              aria-label={t('settingsTimeZone')}
              aria-disabled={update.isPending}
            >
              {zone}
              <ChevronDown size={15} aria-hidden="true" />
            </Popover.Trigger>
            <Popover.Portal>
              <Popover.Content
                className="settings-category-popover settings-time-zone-popover"
                sideOffset={8}
                align="start"
                collisionPadding={16}
                aria-label={t('settingsTimeZone')}
              >
                <label className="settings-search">
                  <Search size={16} aria-hidden="true" />
                  <input
                    aria-label={t('settingsTimeZoneSearch')}
                    placeholder={t('settingsTimeZoneSearch')}
                    value={search}
                    onChange={(event) => setSearch(event.target.value)}
                    onKeyDown={(event) => {
                      if (event.key === 'ArrowDown') {
                        event.preventDefault()
                        event.currentTarget
                          .closest('.settings-time-zone-popover')
                          ?.querySelector<HTMLButtonElement>('.settings-category-option')
                          ?.focus()
                      }
                    }}
                  />
                </label>
                <div
                  className="settings-category-options"
                  onKeyDown={(event) => {
                    const buttons = Array.from(event.currentTarget.querySelectorAll('button'))
                    const index = buttons.indexOf(document.activeElement as HTMLButtonElement)
                    const next =
                      event.key === 'ArrowDown'
                        ? (index + 1) % buttons.length
                        : event.key === 'ArrowUp'
                          ? (index - 1 + buttons.length) % buttons.length
                          : event.key === 'Home'
                            ? 0
                            : event.key === 'End'
                              ? buttons.length - 1
                              : -1
                    if (next >= 0) {
                      event.preventDefault()
                      buttons[next]?.focus()
                    }
                  }}
                >
                  {matches.map((name) => (
                    <button
                      className="settings-category-option"
                      key={name}
                      aria-pressed={zone === name}
                      onClick={() => {
                        update.mutate({ timezone: name, timezone_source: 'manual' })
                        setOpen(false)
                        setSearch('')
                      }}
                    >
                      <span>{name}</span>
                      {zone === name && <Check size={14} aria-hidden="true" />}
                    </button>
                  ))}
                  {!matches.length && <p role="status">{t('settingsTimeZoneNoMatches')}</p>}
                </div>
              </Popover.Content>
            </Popover.Portal>
          </Popover.Root>
          <output
            aria-live="off"
            className="settings-time-zone-clock"
            aria-label={t('settingsTimeZoneNow')}
          >
            {localClock(zone, i18n.language, now)}
          </output>
          {update.isError && <p role="alert">{t('settingsTimeZoneSaveError')}</p>}
        </>
      )}
    </div>
  )
}
