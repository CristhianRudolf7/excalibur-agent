import { useState } from 'react'
import { Popover } from 'radix-ui'
import { Check, ChevronDown, Search } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { SkillGroup } from '@/lib/rpc/contract'

export function Categories({
  groups,
  total,
  value,
  onChange,
}: {
  groups: SkillGroup[]
  total: number
  value: string
  onChange: (value: string) => void
}) {
  const { t, i18n } = useTranslation()
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState('')
  const ranked = [...groups].sort((a, b) => b.count - a.count || a.id.localeCompare(b.id))
  const top = ranked.slice(0, 8)
  const rest = ranked.slice(8)
  const name = (group: SkillGroup) =>
    i18n.language.startsWith('es') ? group.name_es : group.name_en
  const selected = rest.find((group) => group.id === value)
  const matches = rest.filter((group) =>
    name(group).toLocaleLowerCase().includes(search.toLocaleLowerCase()),
  )
  return (
    <div className="skill-category-bar" role="group" aria-label={t('settingsCategories')}>
      <div className="skill-categories">
        <button aria-pressed={!value} onClick={() => onChange('')}>
          {t('settingsAll')} <span>{total}</span>
        </button>
        {top.map((group) => (
          <button
            key={group.id}
            aria-pressed={value === group.id}
            onClick={() => onChange(group.id)}
          >
            {name(group)} <span>{group.count}</span>
          </button>
        ))}
      </div>
      {rest.length > 0 && (
        <Popover.Root
          open={open}
          onOpenChange={(next) => {
            setOpen(next)
            if (!next) setSearch('')
          }}
        >
          <Popover.Trigger className="skill-category-more" data-selected={!!selected}>
            {selected ? name(selected) : t('settingsMoreCategories', { count: rest.length })}
            <ChevronDown size={14} />
          </Popover.Trigger>
          <Popover.Portal>
            <Popover.Content
              className="settings-category-popover"
              sideOffset={8}
              align="end"
              collisionPadding={16}
              aria-label={t('settingsCategories')}
            >
              <label className="settings-search">
                <Search size={16} aria-hidden="true" />
                <input
                  aria-label={t('settingsSearchCategories')}
                  placeholder={t('settingsSearchCategories')}
                  value={search}
                  onChange={(event) => setSearch(event.target.value)}
                  onKeyDown={(event) => {
                    if (event.key === 'ArrowDown') {
                      event.preventDefault()
                      event.currentTarget
                        .closest('[data-radix-popper-content-wrapper]')
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
                {matches.map((group) => (
                  <button
                    className="settings-category-option"
                    key={group.id}
                    aria-pressed={value === group.id}
                    onClick={() => {
                      onChange(group.id)
                      setOpen(false)
                      setSearch('')
                    }}
                  >
                    <span>{name(group)}</span>
                    <small>{group.count}</small>
                    {value === group.id && <Check size={14} />}
                  </button>
                ))}
                {!matches.length && <p role="status">{t('settingsNoCategories')}</p>}
              </div>
            </Popover.Content>
          </Popover.Portal>
        </Popover.Root>
      )}
    </div>
  )
}
