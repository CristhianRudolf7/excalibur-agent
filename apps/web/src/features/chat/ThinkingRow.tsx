import { useAgent } from '@/features/agents/store'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { PixelAvatar } from '@/components/shared'
import { openPanelView } from '@/features/files/open'
import type { AgentId } from '@/features/shell/mock-data'

/** Live "working" row shown as the last message while the agent replies:
 * shimmering status, elapsed time and reassurance when a task takes long. */
export function ThinkingRow({ agentId, activity }: { agentId: AgentId; activity?: string }) {
  const { t } = useTranslation()
  const agent = useAgent(agentId)
  const [seconds, setSeconds] = useState(0)
  const row = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const started = Date.now()
    const timer = setInterval(() => setSeconds(Math.floor((Date.now() - started) / 1000)), 1000)
    row.current?.scrollIntoView?.({ block: 'end', behavior: 'smooth' })
    return () => clearInterval(timer)
  }, [])
  const hint = seconds >= 30 ? 'chatThinkingLong' : seconds >= 10 ? 'chatThinkingWhile' : null
  return (
    <div ref={row} className="thinking-row" role="status" aria-live="polite">
      <span className="thinking-avatar">
        <PixelAvatar id={agentId} size={32} />
      </span>
      <div className="thinking-body">
        <div className="thinking-line">
          <span className="thinking-text">
            {activity ? t(activity) : t('agentThinking', { name: agent.name })}
          </span>
          <span className="thinking-dots" aria-hidden="true">
            <i />
            <i />
            <i />
          </span>
          {seconds >= 3 && <span className="thinking-time">{seconds} s</span>}
        </div>
        <div className="thinking-bar" aria-hidden="true">
          <span />
        </div>
        {hint && (
          <p className="thinking-hint">
            {t(hint)}{' '}
            {seconds >= 30 && (
              <button type="button" onClick={() => openPanelView('activity')}>
                {t('chatThinkingActivity')}
              </button>
            )}
          </p>
        )}
      </div>
    </div>
  )
}
