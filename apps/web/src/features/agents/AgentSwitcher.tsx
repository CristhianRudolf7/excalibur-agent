import { useState } from 'react'
import { DropdownMenu } from 'radix-ui'
import { ChevronDown, Check, Star, MessageCircle } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { AgentAvatar } from '@/components/AgentAvatar'
import { useAgents, useSelectedAgent, orderAgents } from './store'
import { markAgentRead } from './api'

/** Shared by the chat and every tool header; selection stays available across interfaces. */
export function AgentSwitcher({
  selectedId,
  onChange,
}: {
  selectedId?: string
  onChange?: (id: string) => void
}) {
  const { t } = useTranslation()
  const agents = useAgents((state) => state.agents)
  const selected = useSelectedAgent()
  const agent = agents.find((item) => item.id === selectedId && !item.archived) ?? selected
  const [error, setError] = useState(false)
  const choose = (id: string) => {
    setError(false)
    useAgents.getState().select(id)
    onChange?.(id)
    void markAgentRead(id).catch(() => setError(true))
  }
  return (
    <>
      <DropdownMenu.Root>
        <DropdownMenu.Trigger asChild>
          <button
            type="button"
            className="agent-switcher"
            aria-label={t('agentSwitch', { name: agent.name })}
          >
            <AgentAvatar spec={agent.avatar} size={32} />
            <span className="agent-switcher-name">{agent.name}</span>
            <ChevronDown className="agent-switcher-chevron" size={14} aria-hidden="true" />
          </button>
        </DropdownMenu.Trigger>
        <DropdownMenu.Portal>
          <DropdownMenu.Content className="agent-switcher-menu" sideOffset={6} align="end">
            <DropdownMenu.RadioGroup value={agent.id} onValueChange={choose}>
              {orderAgents(agents).map((item) => (
                <DropdownMenu.RadioItem
                  key={item.id}
                  value={item.id}
                  className="agent-switcher-item"
                  aria-label={item.name}
                >
                  <AgentAvatar spec={item.avatar} size={28} />
                  <span>{item.name}</span>
                  {item.favorite && <Star size={12} aria-label={t('agentFavorite')} />}
                  <DropdownMenu.ItemIndicator>
                    <Check size={14} />
                  </DropdownMenu.ItemIndicator>
                </DropdownMenu.RadioItem>
              ))}
            </DropdownMenu.RadioGroup>
          </DropdownMenu.Content>
        </DropdownMenu.Portal>
      </DropdownMenu.Root>
      {error && (
        <span className="agent-error" role="alert">
          {t('agentReadError')}
        </span>
      )}
    </>
  )
}

export function AgentChatLink() {
  const { t } = useTranslation()
  const agent = useSelectedAgent()
  return (
    <a
      className="icon-button"
      href={`/agents/${encodeURIComponent(agent.id)}?view=chat`}
      aria-label={t('agentOpenChat')}
      title={t('agentOpenChat')}
    >
      <MessageCircle size={20} />
    </a>
  )
}
