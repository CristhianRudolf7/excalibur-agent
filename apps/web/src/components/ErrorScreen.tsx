import { Component, useState, type ErrorInfo, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { ChevronDown, Copy, Check, RotateCcw, MessageCircle } from 'lucide-react'

export const errorMessages = {
  errorEyebrow: ['Algo se interrumpió', 'Something interrupted'],
  errorTitle: ['Esta vista tuvo un problema', 'This view hit a problem'],
  errorText: [
    'No perdiste nada: tus conversaciones y automatizaciones siguen guardadas. Puedes reintentar o volver al chat.',
    'Nothing was lost: your conversations and automations are still saved. You can retry or go back to the chat.',
  ],
  errorRetry: ['Reintentar', 'Retry'],
  errorHome: ['Volver al chat', 'Back to chat'],
  errorDetails: ['Detalles técnicos', 'Technical details'],
  errorCopy: ['Copiar detalles', 'Copy details'],
  errorCopied: ['Copiado', 'Copied'],
} satisfies Record<string, [string, string]>

function describe(error: unknown, componentStack?: string) {
  const e = error instanceof Error ? error : new Error(String(error))
  return [
    `${e.name}: ${e.message}`,
    `URL: ${typeof location === 'undefined' ? '' : location.pathname + location.search}`,
    `Time: ${new Date().toISOString()}`,
    '',
    e.stack ?? '',
    componentStack ? `\nComponent stack:${componentStack}` : '',
  ]
    .join('\n')
    .trim()
}

/** Friendly, on-brand error view with retry, a way home and copyable details. */
export function ErrorScreen({
  error,
  reset,
  componentStack,
  compact = false,
}: {
  error: unknown
  reset?: () => void
  componentStack?: string
  compact?: boolean
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [copied, setCopied] = useState(false)
  const details = describe(error, componentStack)
  return (
    <section
      className={compact ? 'error-screen error-screen-compact' : 'error-screen'}
      role="alert"
    >
      <div className="error-card">
        <span className="error-mark" aria-hidden="true">
          <svg viewBox="0 0 48 48" width="40" height="40">
            <circle className="error-ring" cx="24" cy="24" r="19" />
            <path className="error-bolt" d="M26 11 15 27h8l-2 10 12-17h-8l1-9Z" />
          </svg>
        </span>
        <p className="error-eyebrow">{t('errorEyebrow')}</p>
        <h1>{t('errorTitle')}</h1>
        <p className="error-text">{t('errorText')}</p>
        <div className="error-actions">
          <button
            type="button"
            className="error-primary"
            onClick={() => (reset ? reset() : location.reload())}
          >
            <RotateCcw size={16} aria-hidden="true" />
            {t('errorRetry')}
          </button>
          <a className="error-secondary" href="/">
            <MessageCircle size={16} aria-hidden="true" />
            {t('errorHome')}
          </a>
        </div>
        <div className="error-details">
          <button
            type="button"
            className="error-toggle"
            aria-expanded={open}
            onClick={() => setOpen((v) => !v)}
          >
            {t('errorDetails')}
            <ChevronDown size={15} aria-hidden="true" />
          </button>
          {open && (
            <div className="error-log">
              <pre>{details}</pre>
              <button
                type="button"
                className="error-copy"
                onClick={() => {
                  void navigator.clipboard
                    ?.writeText(details)
                    .then(() => {
                      setCopied(true)
                      setTimeout(() => setCopied(false), 1600)
                    })
                    .catch(() => {})
                }}
              >
                {copied ? <Check size={14} /> : <Copy size={14} />}
                {t(copied ? 'errorCopied' : 'errorCopy')}
              </button>
            </div>
          )}
        </div>
      </div>
    </section>
  )
}

/**
 * Keeps a crash inside the content area so the sidebar stays usable.
 * Pass `resetKey` (for example the pathname) to clear the error on navigation.
 */
export class ErrorBoundary extends Component<
  { children: ReactNode; resetKey?: string },
  { error: unknown; stack?: string; key?: string }
> {
  state: { error: unknown; stack?: string; key?: string } = { error: null }
  static getDerivedStateFromError(error: unknown) {
    return { error }
  }
  static getDerivedStateFromProps(
    props: { resetKey?: string },
    state: { error: unknown; key?: string },
  ) {
    if (state.key !== props.resetKey) return { error: null, stack: undefined, key: props.resetKey }
    return null
  }
  componentDidCatch(error: unknown, info: ErrorInfo) {
    console.error('Excalibur view error', error, info.componentStack)
    this.setState({ stack: info.componentStack ?? undefined })
  }
  render() {
    if (this.state.error)
      return (
        <ErrorScreen
          compact
          error={this.state.error}
          componentStack={this.state.stack}
          reset={() => this.setState({ error: null, stack: undefined })}
        />
      )
    return this.props.children
  }
}
