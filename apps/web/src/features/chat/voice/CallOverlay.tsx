import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Mic, Phone, PhoneOff, Volume2 } from 'lucide-react'
import { Dialog } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { PixelAvatar } from '@/components/shared'
import { rpc } from '@/lib/rpc/client'
import { VoiceCall, type CallSnapshot } from './call'
import './voice.css'

export function CallOverlay({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation()
  const [starting, setStarting] = useState(false)
  const [available, setAvailable] = useState<boolean>()
  const [snapshot, setSnapshot] = useState<CallSnapshot>({
    state: 'ready',
    energy: 0,
    transcript: [],
  })
  const [muted, setMuted] = useState(false)
  const [speaker, setSpeaker] = useState(true)
  const [elapsed, setElapsed] = useState(0)
  const call = useRef<VoiceCall>(null)
  const transcript = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const controller = new AbortController()
    void rpc
      .call('voice.status', undefined, { signal: controller.signal })
      .then((status) => {
        if (!controller.signal.aborted) setAvailable(status.available)
      })
      .catch(() => {
        if (!controller.signal.aborted) setAvailable(false)
      })
    return () => {
      controller.abort()
      call.current?.end()
    }
  }, [])
  const active = snapshot.state !== 'ready' && snapshot.state !== 'ended'
  useEffect(() => {
    if (!active) return
    const timer = setInterval(() => setElapsed((value) => value + 1), 1000)
    return () => clearInterval(timer)
  }, [active])
  useEffect(() => {
    transcript.current?.scrollTo?.({
      top: transcript.current.scrollHeight,
      behavior: window.matchMedia('(prefers-reduced-motion: reduce)').matches
        ? 'instant'
        : 'smooth',
    })
  }, [snapshot.transcript])
  const start = () => {
    call.current?.end()
    setElapsed(0)
    setMuted(false)
    setSpeaker(true)
    setStarting(true)
    call.current = new VoiceCall(setSnapshot)
    void call.current.start().finally(() => setStarting(false))
  }
  const states = {
    ready: 'callReady',
    listening: 'callListening',
    transcribing: 'callTranscribing',
    thinking: 'callThinking',
    speaking: 'callSpeaking',
    ended: 'callEnded',
  }
  return (
    <Dialog
      className="call-dialog"
      title={t('callTitle')}
      onClose={() => {
        call.current?.end()
        onClose()
      }}
    >
      <div className="voice-call">
        <div className={`call-avatar call-state-${snapshot.state}`}>
          <PixelAvatar id="excalibur" size={120} />
        </div>
        <strong>Excalibur</strong>
        <time className="call-timer">
          {`${Math.floor(elapsed / 60)}`.padStart(2, '0')}:{`${elapsed % 60}`.padStart(2, '0')}
        </time>
        <p role="status">
          {available === false ? t('callUnavailable') : t(states[snapshot.state])}
        </p>
        {snapshot.error && <p role="alert">{t(snapshot.error)}</p>}
        <div className="call-wave" aria-hidden="true">
          {Array.from({ length: 25 }, (_, index) => (
            <span
              key={index}
              style={{
                transform: `scaleY(${Math.max(0.08, Math.min(1, snapshot.energy * 14 * (1 + Math.sin(index))))})`,
              }}
            />
          ))}
        </div>
        <div
          className="call-transcript"
          ref={transcript}
          role="log"
          aria-label={t('callTranscript')}
        >
          {snapshot.transcript.map((line, index) => (
            <p key={index} className={`call-line-${line.role}`}>
              {line.text}
            </p>
          ))}
        </div>
        <div className="call-controls">
          <Button
            aria-label={t('callMute')}
            aria-pressed={muted}
            disabled={!active}
            onClick={() => {
              call.current?.mute(!muted)
              setMuted(!muted)
            }}
          >
            <Mic />
          </Button>
          <Button
            aria-label={t('callSpeaker')}
            aria-pressed={speaker}
            disabled={!active}
            onClick={() => {
              call.current?.setSpeaker(!speaker)
              setSpeaker(!speaker)
            }}
          >
            <Volume2 />
          </Button>
          {active ? (
            <Button
              className="call-hangup"
              aria-label={t('callHangup')}
              onClick={() => {
                call.current?.end()
                onClose()
              }}
            >
              <PhoneOff />
            </Button>
          ) : (
            <Button
              className="call-start call-start-round"
              aria-label={t('callStart')}
              title={t('callStart')}
              disabled={available !== true || starting}
              onClick={start}
            >
              <Phone />
            </Button>
          )}
        </div>
      </div>
    </Dialog>
  )
}
