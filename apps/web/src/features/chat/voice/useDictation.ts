import { useCallback, useEffect, useRef, useState } from 'react'
import { BrowserRecognizer, recognitionSupported } from './speech'

/**
 * Browser speech-to-text for the composer. While listening, the recognized
 * words are appended live after the text that was in the box when it started.
 */
export function useDictation(lang: string, onText: (text: string) => void) {
  const [listening, setListening] = useState(false)
  const [error, setError] = useState<'callPermission' | 'callError'>()
  const recognizer = useRef<BrowserRecognizer | null>(null)
  const base = useRef('')
  const latest = useRef(onText)
  latest.current = onText
  const supported = recognitionSupported()

  const stop = useCallback(() => {
    recognizer.current?.stop()
    recognizer.current = null
    setListening(false)
  }, [])

  const start = useCallback(
    (current: string) => {
      if (!supported) return
      recognizer.current?.stop()
      base.current = current.trimEnd()
      setError(undefined)
      const instance = new BrowserRecognizer(
        lang,
        () => {
          const heard = instance.peek()
          latest.current(heard ? `${base.current}${base.current ? ' ' : ''}${heard}` : base.current)
        },
        (failure) => {
          setError(failure)
          stop()
        },
      )
      recognizer.current = instance
      instance.start()
      setListening(true)
    },
    [lang, stop, supported],
  )

  useEffect(() => () => recognizer.current?.stop(), [])
  return { supported, listening, error, start, stop }
}
