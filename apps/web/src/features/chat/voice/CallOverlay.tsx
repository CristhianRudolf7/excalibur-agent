import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Mic, Phone, PhoneOff, Volume2 } from 'lucide-react'
import { Dialog } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { PixelAvatar } from '@/components/shared'
import { rpc } from '@/lib/rpc/client'
import { VoiceCall, type CallSnapshot, type VoiceMode } from './call'
import { browserVoiceSupported, speechLang } from './speech'
import './voice.css'

export function CallOverlay({ onClose }: { onClose: () => void }) {
  const { t, i18n } = useTranslation()
  const [mode, setMode] = useState<VoiceMode>()
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
      .then((status) => status.available)
      .catch(() => false)
      .then((server) => {
        if (controller.signal.aborted) return
        // Without voice credentials the call runs on the browser's own STT and TTS.
        const next: VoiceMode | undefined = server
          ? 'server'
          : browserVoiceSupported()
            ? 'browser'
            : undefined
        setMode(next)
        setAvailable(next !== undefined)
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
    call.current = new VoiceCall(setSnapshot, { mode, lang: speechLang(i18n.language) })
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
        {(snapshot.mode ?? mode) === 'browser' && available && (
          <p className="call-mode">{t('callBrowserMode')}</p>
        )}
        {snapshot.notice && <p className="call-notice">{t(snapshot.notice)}</p>}
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
          {snapshot.partial && (
            <p className="call-line-user call-line-partial">{snapshot.partial}</p>
          )}
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
