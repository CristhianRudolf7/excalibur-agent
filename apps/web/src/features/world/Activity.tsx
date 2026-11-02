import { useRef, useState } from 'react'
import { useVirtualizer } from '@tanstack/react-virtual'
import { ChevronDown } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { PixelPortrait } from '@/components/PixelSprite'
import { filterActivity } from './events'
import { formatWorldTime } from './format'
import type { WorldAgent, WorldEvent } from './events'
import './world.css'
export interface ActivityFeedProps {
  entries: readonly WorldEvent[]
  agents: Record<string, WorldAgent>
  roomLabels: Record<string, string>
  roomId?: string
  collapsed: boolean
  unread: number
  inline?: boolean
  onToggle: () => void
  onSelect: (event: WorldEvent) => void
}
/** Controlled, virtualized feed. Events, identities, labels, collapse state and navigation come from the caller; inline defaults to normal document flow for embedding in other views. */
export function ActivityFeed({
  entries,
  agents,
  roomLabels,
  roomId,
  collapsed,
  unread,
  onToggle,
  inline = true,
  onSelect,
}: ActivityFeedProps) {
  const { t, i18n } = useTranslation(),
    [all, setAll] = useState(false)
  const scroll = useRef<HTMLDivElement>(null)
  const filtered = filterActivity(entries, all ? undefined : roomId)
  // oxlint-disable-next-line react/incompatible-library -- Virtualizer measurements remain outside React Compiler memoization.
  const virtual = useVirtualizer({
    count: collapsed ? 0 : filtered.length,
    getScrollElement: () => scroll.current,
    estimateSize: () => 76,
    overscan: 3,
  })
  return (
    <section
      className={`world-activity world-panel ${collapsed ? 'is-collapsed' : ''}`}
      style={inline ? { position: 'relative', inset: 'auto', width: '100%' } : undefined}
      aria-label={t('worldActivity')}
    >
      <button className="world-panel-title" aria-expanded={!collapsed} onClick={onToggle}>
        <span className="world-live-dot" aria-hidden="true" />
        <strong>{t('worldActivity')}</strong>
        <span className="world-badge">{collapsed ? unread : filtered.length}</span>
        <ChevronDown size={14} />
      </button>
      {!collapsed && (
        <>
          {roomId && (
            <button className="world-all" aria-pressed={all} onClick={() => setAll(!all)}>
              {t('worldAll')}
            </button>
          )}
          <div className="world-activity-scroll" ref={scroll}>
            {!filtered.length && <p className="world-activity-empty">{t('worldActivityEmpty')}</p>}
            <div style={{ height: virtual.getTotalSize(), position: 'relative' }}>
              {virtual.getVirtualItems().map((item) => {
                const event = filtered[item.index]!,
                  agent = agents[event.agentId]
                return (
                  <button
                    key={event.id}
                    className="world-activity-entry"
                    style={{
                      position: 'absolute',
                      width: '100%',
                      top: item.start,
                      height: item.size,
                    }}
                    onClick={() => onSelect(event)}
                  >
                    {agent && <PixelPortrait sprite={agent.sprite} size={26} />}
                    <span>
                      <span>{event.literal ? event.text : t(event.text)}</span>
                      <small>
                        {t('worldActivityMeta', {
                          time: formatWorldTime(event.timestamp, i18n.language),
                          room: t(roomLabels[event.target.roomId ?? ''] ?? 'worldCampus'),
                        })}
                      </small>
                    </span>
                  </button>
                )
              })}
            </div>
          </div>
        </>
      )}
    </section>
  )
}
