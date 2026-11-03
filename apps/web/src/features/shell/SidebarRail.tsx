import { Tooltip } from 'radix-ui'
import {
  Bot,
  ClipboardCheck,
  Bell,
  Folder,
  Globe,
  GraduationCap,
  Store,
  Wrench,
  Workflow,
  Settings,
  LoaderCircle,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Fragment, type ReactNode } from 'react'
import { IconButton, PixelAvatar } from '@/components/shared'
import { ProfileButton } from '@/features/profile/Profile'
import type { Agent, AgentId } from './mock-data'

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
  onAgent: (id: AgentId) => void
  onProfile: () => void
}) {
  const { t } = useTranslation()
  const nav = [
    { key: 'myTasks', icon: ClipboardCheck },
    { key: 'reminders', icon: Bell },
    { key: 'projects', icon: Folder },
    { key: 'world', icon: Globe },
    { key: 'classroom', icon: GraduationCap },
    { key: 'robot', icon: Bot },
  ]
  return (
    <Tooltip.Provider delayDuration={200}>
      <aside className="sidebar-rail" aria-label={t('sidebarRail')}>
        <span className="sidebar-rail-brand" title={t('workspace')}>
          <img className="sidebar-rail-logo" src="/paladin.svg" alt={t('workspace')} />
        </span>
        <nav className="sidebar-rail-agents" aria-label={t('agents')}>
          {agents.map((agent) => (
            <RailTooltip label={t(agent.name, { defaultValue: agent.name })} key={agent.id}>
              <button
                type="button"
                className={`sidebar-rail-agent ${selectedAgentId === agent.id ? 'selected' : ''}`}
                aria-label={t(agent.name, { defaultValue: agent.name })}
                aria-pressed={selectedAgentId === agent.id}
                onClick={() => onAgent(agent.id)}
              >
                <PixelAvatar id={agent.id} size={30} />
                <i className="online-dot" />
                {agent.working ? (
                  <LoaderCircle className="sidebar-rail-working" aria-label={t('working')} />
                ) : agent.unread ? (
                  <span className="sidebar-rail-unread">{agent.unread}</span>
                ) : null}
              </button>
            </RailTooltip>
          ))}
        </nav>
        <div className="sidebar-rail-divider" />
        <nav className="sidebar-rail-nav" aria-label={t('navigation')}>
          {nav.map(({ key, icon: Icon }) => (
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
        <div className="sidebar-rail-bottom">
          {[
            { key: 'stores', icon: Store },
            { key: 'tools', icon: Wrench },
            { key: 'automations', icon: Workflow },
            { key: 'lab_title', icon: Bot },
            { key: 'settings', icon: Settings },
          ].map(({ key, icon: Icon }) => (
            <Fragment key={key}>
              {key === 'lab_title' && <div className="sidebar-rail-divider" />}
              <RailTooltip label={t(key)}>
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
            </Fragment>
          ))}
          <RailTooltip label={t('profileTitle')}>
            <ProfileButton avatarOnly onClick={onProfile} />
          </RailTooltip>
        </div>
      </aside>
    </Tooltip.Provider>
  )
}
