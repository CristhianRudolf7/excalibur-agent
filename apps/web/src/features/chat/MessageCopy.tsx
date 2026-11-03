import { useEffect, useRef, useState } from 'react'
import { Check, Copy } from 'lucide-react'
import { useTranslation } from 'react-i18next'

export function MessageCopy({ text }: { text: string }) {
  const { t } = useTranslation()
  const [feedback, setFeedback] = useState<'idle' | 'copied' | 'failed'>('idle')
  const mounted = useRef(false)
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  useEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
      clearTimeout(timer.current)
    }
  }, [])
  return (
    <button
      type="button"
      className="message-copy"
      aria-label={t('chatCopyMessage')}
      title={t(feedback === 'copied' ? 'markdownCopied' : 'chatCopyMessage')}
      onClick={async () => {
        clearTimeout(timer.current)
        try {
          await navigator.clipboard.writeText(text)
          if (!mounted.current) return
          setFeedback('copied')
        } catch {
          if (!mounted.current) return
          setFeedback('failed')
        }
        timer.current = setTimeout(() => setFeedback('idle'), 1500)
      }}
    >
      {feedback === 'copied' ? <Check size={14} /> : <Copy size={14} />}
      <span className="sr-only" aria-live="polite">
        {feedback === 'idle'
          ? t('markdownCopy')
          : t(feedback === 'copied' ? 'markdownCopied' : 'markdownCopyFailed')}
      </span>
    </button>
  )
}
