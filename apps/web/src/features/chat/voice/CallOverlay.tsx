import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Mic, MicOff, PhoneOff, Volume2, VolumeX } from 'lucide-react'
import { Dialog } from '@/components/ui/dialog'
import { PixelAvatar } from '@/components/shared'
import { callActive, useCall } from './callStore'
import './voice.css'

const stateLabels = {
  ready: 'callConnecting',
  listening: 'callListening',
  transcribing: 'callTranscribing',
  thinking: 'callThinking',
  speaking: 'callSpeaking',
  ended: 'callEnded',
} as const

/** Seconds since the call started, refreshed every second while it is live. */
export function useCallElapsed() {
  const startedAt = useCall((state) => state.startedAt)
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    if (!startedAt) return
    const timer = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(timer)
  }, [startedAt])
  const seconds = startedAt ? Math.max(0, Math.floor((now - startedAt) / 1000)) : 0
  return `${Math.floor(seconds / 60)}`.padStart(2, '0') + ':' + `${seconds % 60}`.padStart(2, '0')
}

/** Mute, speaker and hang-up: shared by the overlay and the chat composer. */
export function CallControls({ compact = false }: { compact?: boolean }) {
  const { t } = useTranslation()
  const { muted, speaker, toggleMute, toggleSpeaker, hangup } = useCall()
  return (
    <div className={compact ? 'call-controls call-controls-compact' : 'call-controls'}>
      <button
        type="button"
        className="call-control"
        aria-label={t('callMute')}
        title={t('callMute')}
        aria-pressed={muted}
        onClick={toggleMute}
      >
        {muted ? <MicOff /> : <Mic />}
      </button>
      <button
        type="button"
        className="call-control"
        aria-label={t('callSpeaker')}
        title={t('callSpeaker')}
        aria-pressed={speaker}
        onClick={toggleSpeaker}
      >
        {speaker ? <Volume2 /> : <VolumeX />}
      </button>
      <button
        type="button"
        className="call-control call-hangup"
        aria-label={t('callHangup')}
        title={t('callHangup')}
        onClick={hangup}
      >
        <PhoneOff />
      </button>
    </div>
  )
}

/** Live voice view. Closing it only hides it: the call goes on until hang-up. */
export function CallOverlay() {
  const { t } = useTranslation()
  const { snapshot, overlay, connecting, unavailable, muted, hide } = useCall()
  const elapsed = useCallElapsed()
  if (!overlay) return null
  const live = callActive(snapshot)
  const state = connecting && !live ? 'ready' : snapshot.state
  const level = muted ? 0 : Math.min(1, snapshot.energy * 9)
  const lastLine = snapshot.partial
    ? { role: 'user' as const, text: snapshot.partial }
    : snapshot.transcript.at(-1)
  return (
    <Dialog className="call-dialog" title={t('callTitle')} onClose={hide} footer={false}>
      <div className={`voice-call call-state-${state}`}>
        <div
          className="call-orb"
          aria-hidden="true"
          style={{ '--level': level.toFixed(3) } as React.CSSProperties}
        >
          <span className="call-orb-halo" />
          <span className="call-orb-ring" />
          <span className="call-orb-core">
            <PixelAvatar id="excalibur" size={132} />
          </span>
        </div>
        <p className="call-status" role="status">
          <span>{unavailable ? t('callUnavailable') : t(stateLabels[state])}</span>
          {live && <time className="call-timer"> · {elapsed}</time>}
        </p>
        {snapshot.notice && <p className="call-notice">{t(snapshot.notice)}</p>}
        {snapshot.error && <p role="alert">{t(snapshot.error)}</p>}
        <div className="call-caption" role="log" aria-label={t('callTranscript')}>
          {snapshot.transcript.map((line, index) => (
            <p key={index} className={`call-line-${line.role}`} hidden={line !== lastLine}>
              {line.text}
            </p>
          ))}
          {snapshot.partial && (
            <p className="call-line-user call-line-partial">{snapshot.partial}</p>
          )}
        </div>
        <CallControls />
      </div>
    </Dialog>
  )
}
