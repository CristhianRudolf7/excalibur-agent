import { useEffect, useState } from 'react'
import { Square, Volume2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { pickVoice, speakableText, speechLang } from './voice/speech'

// One browser speech at a time; buttons subscribe to know which one is live.
let current: string | null = null
const listeners = new Set<(id: string | null) => void>()
function setCurrent(id: string | null) {
  current = id
  listeners.forEach((fn) => fn(id))
}

export { speakableText }

export function MessageSpeak({ id, text }: { id: string; text: string }) {
  const { t, i18n } = useTranslation()
  const [live, setLive] = useState(current === id)
  const supported = typeof window !== 'undefined' && 'speechSynthesis' in window
  useEffect(() => {
    const fn = (active: string | null) => setLive(active === id)
    listeners.add(fn)
    return () => {
      listeners.delete(fn)
      if (current === id) {
        window.speechSynthesis?.cancel()
        setCurrent(null)
      }
    }
  }, [id])
  if (!supported) return null
  return (
    <button
      type="button"
      className={`message-speak ${live ? 'speaking' : ''}`}
      aria-label={t(live ? 'chatStopSpeaking' : 'chatSpeak')}
      aria-pressed={live}
      title={t(live ? 'chatStopSpeaking' : 'chatSpeak')}
      onClick={() => {
        const synth = window.speechSynthesis
        synth.cancel()
        if (live) {
          setCurrent(null)
          return
        }
        const utterance = new SpeechSynthesisUtterance(speakableText(text))
        utterance.lang = speechLang(i18n.language)
        const voice = pickVoice(utterance.lang)
        if (voice) utterance.voice = voice
        utterance.onend = () => current === id && setCurrent(null)
        utterance.onerror = () => current === id && setCurrent(null)
        setCurrent(id)
        synth.speak(utterance)
      }}
    >
      {live ? <Square size={13} fill="currentColor" /> : <Volume2 size={15} />}
    </button>
  )
}
