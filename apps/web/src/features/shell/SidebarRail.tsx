import { Tooltip } from 'radix-ui'
import { Puzzle, Settings } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { ReactNode } from 'react'
import { IconButton } from '@/components/shared'
import { ProfileButton } from '@/features/profile/Profile'
import type { Agent } from '@/features/agents/types'
import { AgentAvatar } from '@/components/AgentAvatar'
import { orderAgents } from '@/features/agents/store'
import { extensionCatalog } from '@/features/extensions/catalog'
import { useExtensions } from '@/features/extensions/store'
import { primaryNavigation } from './navigation'
import './navigation.css'

function RailTooltip({ label, children }: { label: string; children: ReactNode }) {
  return (
    <Tooltip.Root>
      <Tooltip.Trigger asChild>{children}</Tooltip.Trigger>
      <Tooltip.Portal>
        <Tooltip.Content className="sidebar-rail-tooltip" side="right" sideOffset={8}>
          {label}
          <Tooltip.Arrow />
        </Tooltip.Content>
      </Tooltip.Portal>
    </Tooltip.Root>
  )
}
/** Shared navigation for World and a collapsed desktop sidebar. */
export function SidebarRail({
  agents,
  selectedAgentId,
  activeItem,
  onNavigate,
  onAgent,
  onProfile,
}: {
  agents: readonly Agent[]
  selectedAgentId?: string
  activeItem?: string
  onNavigate: (item: string) => void
  onAgent: (id: string) => void
  onProfile: () => void
}) {
  const { t } = useTranslation()
  const installed = useExtensions((state) => state.installed)
  return (
    <Tooltip.Provider delayDuration={200}>
      <aside className="sidebar-rail" aria-label={t('sidebarRail')}>
        <span className="sidebar-rail-brand" title={t('workspace')}>
          <img className="sidebar-rail-logo" src="/paladin.svg" alt={t('workspace')} />
        </span>
        <nav className="sidebar-rail-agents" aria-label={t('agents')}>
          {orderAgents(agents).map((agent) => (
            <RailTooltip label={t(agent.name, { defaultValue: agent.name })} key={agent.id}>
              <button
                type="button"
                className={`sidebar-rail-agent ${selectedAgentId === agent.id ? 'selected' : ''}`}
                aria-label={t(agent.name, { defaultValue: agent.name })}
                aria-pressed={selectedAgentId === agent.id}
                onClick={() => onAgent(agent.id)}
              >
                <AgentAvatar spec={agent.avatar} size={30} />
                <i className="online-dot" />
                {agent.unread ? <span className="sidebar-rail-unread">{agent.unread}</span> : null}
              </button>
            </RailTooltip>
          ))}
        </nav>
        <div className="sidebar-rail-divider sidebar-agent-divider" role="separator" />
        <nav className="sidebar-rail-nav" aria-label={t('navigation')}>
          {primaryNavigation.map(({ key, icon: Icon }) => (
            <RailTooltip label={t(key)} key={key}>
              <IconButton
                label={key}
                title={undefined}
                aria-pressed={activeItem === key}
                aria-current={activeItem === key ? 'page' : undefined}
                onClick={() => onNavigate(key)}
              >
                <Icon />
              </IconButton>
            </RailTooltip>
          ))}
        </nav>
        <div className="sidebar-rail-divider" role="separator" />
        <nav className="sidebar-rail-nav sidebar-rail-extensions" aria-label={t('extensions')}>
          <RailTooltip label={t('extensions')}>
            <IconButton
              label="extensions"
              title={undefined}
              aria-pressed={activeItem === 'extensions'}
              aria-current={activeItem === 'extensions' ? 'page' : undefined}
              onClick={() => onNavigate('extensions')}
            >
              <Puzzle />
            </IconButton>
          </RailTooltip>
          <div
            className="sidebar-rail-installed"
            role="group"
            aria-label={t('extensionsInstalled')}
          >
            {extensionCatalog
              .filter(({ id }) => installed.includes(id))
              .map((extension) => {
                const Icon = extension.icon
                return (
                  <RailTooltip label={t(extension.nameKey)} key={extension.id}>
                    <IconButton
                      label={extension.nameKey}
                      title={undefined}
                      aria-pressed={activeItem === extension.navigationKey}
                      aria-current={activeItem === extension.navigationKey ? 'page' : undefined}
                      onClick={() => onNavigate(extension.navigationKey)}
                    >
                      <Icon />
                    </IconButton>
                  </RailTooltip>
                )
              })}
          </div>
        </nav>
        <div className="sidebar-rail-bottom">
          <div className="sidebar-rail-divider" role="separator" />
          <nav className="sidebar-rail-nav" aria-label={t('settings')}>
            <RailTooltip label={t('settings')}>
              <IconButton
                label="settings"
                title={undefined}
                aria-pressed={activeItem === 'settings'}
                aria-current={activeItem === 'settings' ? 'page' : undefined}
                onClick={() => onNavigate('settings')}
              >
                <Settings />
              </IconButton>
            </RailTooltip>
          </nav>
          <RailTooltip label={t('profileTitle')}>
            <ProfileButton avatarOnly onClick={onProfile} />
          </RailTooltip>
        </div>
      </aside>
    </Tooltip.Provider>
  )
}
