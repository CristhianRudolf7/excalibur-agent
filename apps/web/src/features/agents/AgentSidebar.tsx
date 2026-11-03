import { useRef } from 'react'
import { useVirtualizer } from '@tanstack/react-virtual'
import { Star, Plus } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { AgentAvatar } from '@/components/AgentAvatar'
import { orderAgents } from './store'
import type { Agent } from './types'

export function AgentSidebar({
  agents,
  disabled = false,
  selectedId,
  onSelect,
  onCreate,
}: {
  agents: readonly Agent[]
  disabled?: boolean
  selectedId?: string
  onSelect: (id: string) => void
  onCreate: () => void
}) {
  const { t, i18n } = useTranslation()
  const ordered = orderAgents(agents)
  const parent = useRef<HTMLDivElement>(null)
  const virtualized = ordered.length > 20
  // oxlint-disable-next-line react/incompatible-library -- Measurements remain outside React Compiler memoization.
  const virtual = useVirtualizer({
    count: virtualized ? ordered.length : 0,
    getScrollElement: () => parent.current,
    estimateSize: () => 48,
    overscan: 4,
  })
  const row = (agent: Agent) => {
    const date = new Date(agent.last_message_at ?? NaN)
    return (
      <button
        type="button"
        key={agent.id}
        className={`agent-chat-row ${selectedId === agent.id ? 'selected' : ''}`}
        aria-label={agent.name}
        disabled={disabled}
        aria-current={selectedId === agent.id ? 'page' : undefined}
        onClick={() => onSelect(agent.id)}
      >
        <AgentAvatar spec={agent.avatar} size={30} />
        <span className="agent-chat-name" title={agent.name}>
          {agent.name}
        </span>
        {agent.favorite && (
          <span
            className="agent-chat-favorite"
            role="img"
            aria-label={t('agentFavorite')}
            title={t('agentFavorite')}
          >
            <Star size={10} aria-hidden="true" />
          </span>
        )}
        <span className="agent-chat-meta">
          {Number.isFinite(date.getTime()) && (
            <time dateTime={agent.last_message_at ?? undefined}>
              {new Intl.DateTimeFormat(i18n.language, {
                hour: '2-digit',
                minute: '2-digit',
              }).format(date)}
            </time>
          )}
          {agent.unread > 0 && (
            <span className="unread" aria-label={t('agentUnread', { count: agent.unread })}>
              {agent.unread}
            </span>
          )}
        </span>
      </button>
    )
  }
  return (
    <>
      <div className="agent-sidebar-scroll" ref={parent}>
        <nav className="agent-nav personal-agent-nav" aria-label={t('agents')}>
          {virtualized ? (
            <div style={{ height: virtual.getTotalSize(), position: 'relative' }}>
              {virtual.getVirtualItems().map((item) => (
                <div
                  key={ordered[item.index]!.id}
                  style={{
                    position: 'absolute',
                    width: '100%',
                    height: item.size,
                    transform: `translateY(${item.start}px)`,
                  }}
                >
                  {row(ordered[item.index]!)}
                </div>
              ))}
            </div>
          ) : (
            ordered.map(row)
          )}
        </nav>
      </div>
      <button type="button" className="agent-create-button" onClick={onCreate} disabled={disabled}>
        <Plus size={16} aria-hidden="true" />
        {t('agentCreate')}
      </button>
    </>
  )
}
