import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { PixelPortrait } from '@/components/PixelSprite'
import type { WorldAgent, WorldEvent } from './events'
export interface RpgDialogProps {
  event: WorldEvent
  agent?: WorldAgent
  count: number
  onAdvance: () => void
  dismissAfter?: number
}
/** One message with a supplied portrait, queue count and advance callback; dismissAfter controls the hover/focus-paused timer. */
export function RpgDialog({ event, agent, count, onAdvance, dismissAfter = 8000 }: RpgDialogProps) {
  const { t } = useTranslation()
  const [letters, setLetters] = useState(0),
    [hovered, setHovered] = useState(false),
    [focused, setFocused] = useState(false)
  const [reduced, setReduced] = useState(
    () => window.matchMedia('(prefers-reduced-motion: reduce)').matches,
  )
  const text = event.literal ? event.text : t(event.text),
    name = agent ? t(agent.name) : t('worldUnknownAgent')
  const complete = reduced || letters >= text.length
  useEffect(() => {
    const media = window.matchMedia('(prefers-reduced-motion: reduce)')
    const change = () => setReduced(media.matches)
    media.addEventListener?.('change', change)
    return () => media.removeEventListener?.('change', change)
  }, [])
  useEffect(() => {
    if (reduced) return
    const timer = setInterval(
      () =>
        setLetters((value) => {
          if (value + 3 >= text.length) clearInterval(timer)
          return value + 3
        }),
      30,
    )
    return () => clearInterval(timer)
  }, [text.length, reduced])
  useEffect(() => {
    if (hovered || focused) return
    const timer = setTimeout(onAdvance, dismissAfter)
    return () => clearTimeout(timer)
  }, [hovered, focused, onAdvance, dismissAfter])
  const next = () => (complete ? onAdvance() : setLetters(text.length))
  useEffect(() => {
    const key = (event: KeyboardEvent) => {
      if (
        event.target instanceof HTMLElement &&
        (event.target.matches('input, textarea, button, select') || event.target.isContentEditable)
      )
        return
      if (event.key === ' ' || event.key === 'Enter') {
        event.preventDefault()
        next()
      }
    }
    window.addEventListener('keydown', key)
    return () => window.removeEventListener('keydown', key)
  })
  return (
    <section
      className="world-dialog"
      aria-label={t('worldContinue')}
      aria-live="polite"
      aria-atomic="true"
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
      onFocus={() => setFocused(true)}
      onBlur={() => setFocused(false)}
    >
      <span className="sr-only">
        {name}: {text}
      </span>
      <button onClick={next} aria-label={t('worldContinue')} className="world-dialog-button">
        {agent && <PixelPortrait sprite={agent.sprite} size={112} label={name} />}
        <span className="world-dialog-content" aria-hidden="true">
          <strong>{name}</strong>
          <span>
            {complete ? text : text.slice(0, letters)}
            <span className={complete ? '' : 'world-caret'} />
          </span>
        </span>
        <span className="world-dialog-count">
          {t('worldQueue', { current: 1, count })}
          <span aria-hidden="true">▼</span>
        </span>
      </button>
    </section>
  )
}
/** Presents the head of an externally managed FIFO queue; knows neither event producers nor application stores. */
export function DialogQueue({
  messages,
  agents,
  onAdvance,
}: {
  messages: readonly WorldEvent[]
  agents: Record<string, WorldAgent>
  onAdvance: () => void
}) {
  const event = messages[0]
  return event ? (
    <RpgDialog
      key={event.id}
      event={event}
      agent={agents[event.agentId]}
      count={messages.length}
      onAdvance={onAdvance}
    />
  ) : null
}
