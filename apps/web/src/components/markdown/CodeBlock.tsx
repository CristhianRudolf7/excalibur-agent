import { lazy, Suspense, useEffect, useRef, useState } from 'react'
import { Check, Copy } from 'lucide-react'
import { useTranslation } from 'react-i18next'

const HighlightedCode = lazy(() => import('./HighlightedCode'))
export function CodeBlock({ code, language }: { code: string; language: string }) {
  const { t } = useTranslation()
  const root = useRef<HTMLDivElement>(null)
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const mounted = useRef(false)
  const [feedback, setFeedback] = useState<'idle' | 'copied' | 'failed'>('idle')
  const [visible, setVisible] = useState(() => typeof IntersectionObserver === 'undefined')
  useEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
      clearTimeout(timer.current)
    }
  }, [])
  useEffect(() => {
    if (visible || !root.current) return
    const observer = new IntersectionObserver((entries) => {
      if (entries.some((entry) => entry.isIntersecting)) {
        setVisible(true)
        observer.disconnect()
      }
    })
    observer.observe(root.current)
    return () => observer.disconnect()
  }, [visible])
  const plain = <code>{code}</code>
  return (
    <div className="markdown-code-block" ref={root}>
      <div className="markdown-code-toolbar">
        <span>{language || t('markdownPlain')}</span>
        <button
          type="button"
          onClick={async () => {
            clearTimeout(timer.current)
            try {
              await navigator.clipboard.writeText(code)
              if (!mounted.current) return
              setFeedback('copied')
            } catch {
              if (!mounted.current) return
              setFeedback('failed')
            }
            timer.current = setTimeout(() => setFeedback('idle'), 1500)
          }}
        >
          {feedback === 'copied' ? <Check size={13} /> : <Copy size={13} />}
          <span aria-live="polite">
            {t(
              feedback === 'copied'
                ? 'markdownCopied'
                : feedback === 'failed'
                  ? 'markdownCopyFailed'
                  : 'markdownCopy',
            )}
          </span>
        </button>
      </div>
      <pre
        tabIndex={0}
        aria-label={t('markdownCode', { language: language || t('markdownPlain') })}
      >
        {visible ? (
          <Suspense fallback={plain}>
            <HighlightedCode code={code} language={language} />
          </Suspense>
        ) : (
          plain
        )}
      </pre>
    </div>
  )
}
