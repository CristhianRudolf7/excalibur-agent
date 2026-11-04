import { useEffect, useRef, useState } from 'react'
import { Globe, LockKeyhole } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { emptyBrowser, useBrowserLive } from './store'
import { activityLabel } from './activityLabel'
import { useBrowserFrames } from './useBrowserFrames'
import type { BrowserActivityEvent } from './types'
import './browserlive.css'

function useAction(activity?: BrowserActivityEvent) {
  const [label, setLabel] = useState<BrowserActivityEvent>()
  const [box, setBox] = useState<BrowserActivityEvent['box']>()
  const shownAt = useRef(0)
  const labelTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const boxTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  useEffect(() => {
    if (!activity || activity.kind === 'page') return
    clearTimeout(boxTimer.current)
    if (activity.phase !== 'start') {
      boxTimer.current = setTimeout(() => setBox(undefined), activity.phase === 'done' ? 500 : 0)
      return
    }
    // A timed projection of bus events intentionally retains completed actions.
    // oxlint-disable-next-line react/set-state-in-effect
    setBox(activity.box)
    clearTimeout(labelTimer.current)
    const show = () => {
      shownAt.current = Date.now()
      setLabel(activity)
    }
    const delay = Math.max(0, shownAt.current + 700 - Date.now())
    if (delay === 0) show()
    else labelTimer.current = setTimeout(show, delay)
  }, [activity])
  useEffect(
    () => () => {
      clearTimeout(labelTimer.current)
      clearTimeout(boxTimer.current)
    },
    [],
  )
  return { label, box }
}
export function BrowserLivePanel({ conversation }: { conversation: string }) {
  const state = useBrowserLive((s) => s.conversations[conversation]) ?? emptyBrowser
  return <BrowserSessionPanel key={state.session?.id ?? conversation} conversation={conversation} />
}
function BrowserSessionPanel({ conversation }: { conversation: string }) {
  const { t } = useTranslation()
  const { session, activity } = useBrowserLive((s) => s.conversations[conversation]) ?? emptyBrowser
  const { canvas, frame } = useBrowserFrames(conversation, session)
  const { label, box } = useAction(activity)
  const ended = session?.state === 'ended'
  let host = '',
    path = session?.url ?? ''
  try {
    const url = new URL(session?.url ?? '')
    host = url.host
    path = url.pathname
  } catch {
    /* An empty URL is normal before navigation. */
  }
  const Icon = session?.url?.startsWith('https:') ? LockKeyhole : Globe
  return (
    <section className={`browser-live ${ended ? 'browser-live-ended' : ''}`}>
      <header className="browser-live-header">
        <Icon size={15} aria-hidden="true" />
        <div className="browser-live-location">
          <div className="browser-live-url" title={session?.url}>
            <strong>{host}</strong>
            <span>{path}</span>
          </div>
          <div className="browser-live-title">{session?.title}</div>
        </div>
        <span className="browser-live-badge">
          <i aria-hidden="true" />
          {t(ended ? 'browserEnded' : 'browserLive')}
        </span>
      </header>
      <div
        className="browser-live-stage"
        style={{ aspectRatio: frame ? `${frame.header.width} / ${frame.header.height}` : '16 / 9' }}
      >
        <canvas
          ref={canvas}
          role="img"
          aria-label={t('browserFrame')}
          hidden={!frame || !!frame.url}
          draggable={false}
        />
        {frame?.url && <img src={frame.url} alt={t('browserFrame')} draggable={false} />}
        {!frame && (
          <div className="browser-live-skeleton" role="status">
            {t('browserConnecting')}
          </div>
        )}
        {box && frame && !ended && (
          <div
            data-testid="browser-highlight"
            className="browser-live-highlight"
            style={{
              left: `${(box.x / frame.header.width) * 100}%`,
              top: `${(box.y / frame.header.height) * 100}%`,
              width: `${(box.width / frame.header.width) * 100}%`,
              height: `${(box.height / frame.header.height) * 100}%`,
            }}
          />
        )}
      </div>
      {!ended && (
        <div className="browser-live-action" aria-live="polite">
          {label ? activityLabel(label, t) : ''}
        </div>
      )}
      {ended && (
        <p className="browser-live-notice" role="status">
          {t('browserEndedNotice')}
        </p>
      )}
    </section>
  )
}
