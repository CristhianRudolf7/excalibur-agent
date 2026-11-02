import { ChevronDown } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { PixelPortrait } from '@/components/PixelSprite'
import { StatusIcon } from '@/components/shared'
import type { Mission } from './layout'
import type { WorldAgent } from './events'
/** Compact mission view. The caller supplies identities, progress/status, collapse state and its toggle action. */
export function MissionBoard({
  missions,
  agents,
  collapsed,
  onToggle,
}: {
  missions: readonly Mission[]
  agents: Record<string, WorldAgent>
  collapsed: boolean
  onToggle: () => void
}) {
  const { t } = useTranslation()
  return (
    <section className="world-missions world-panel" aria-label={t('worldMissions')}>
      <button className="world-panel-title" aria-expanded={!collapsed} onClick={onToggle}>
        <strong>
          <span className="world-title-full">{t('worldMissions')}</span>
          <span className="world-title-short">{t('worldMissionsShort')}</span>
        </strong>
        <span className="world-badge">{missions.length}</span>
        <ChevronDown size={14} />
      </button>
      {!collapsed && (
        <div className="world-mission-rows">
          {missions.map((mission) => (
            <div className="world-mission" key={mission.id}>
              <span className="world-mission-avatars">
                {mission.agentIds.map((id) =>
                  agents[id] ? (
                    <PixelPortrait key={id} sprite={agents[id].sprite} size={28} />
                  ) : null,
                )}
              </span>
              <span>
                <span>{t(mission.title)}</span>
                <progress max={100} value={mission.progress} aria-label={t(mission.title)} />
              </span>
              <span className="world-mission-status">
                <StatusIcon status={mission.status} size={16} />
                <small>{t('worldPercent', { value: mission.progress })}</small>
              </span>
            </div>
          ))}
          {!missions.length && <p className="world-activity-empty">{t('worldNoMissions')}</p>}
        </div>
      )}
    </section>
  )
}
