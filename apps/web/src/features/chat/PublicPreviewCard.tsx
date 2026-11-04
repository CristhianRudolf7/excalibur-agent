import { useEffect, useState } from 'react'
import { Check, Copy, Globe } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Markdown } from '@/components/markdown/Markdown'
import type { SourceRef } from '@/features/library/types'
import { extractPublicPreviews, type PublicPreview } from './public-preview'

export function PublicPreviewCard({ preview }: { preview: PublicPreview }) {
  const { t, i18n } = useTranslation()
  const [copied, setCopied] = useState(false)
  const [copyError, setCopyError] = useState(false)
  const [now, setNow] = useState(() => Date.now())
  const expired = Date.parse(preview.expiresAt) <= now
  useEffect(() => {
    const delay = Date.parse(preview.expiresAt) - Date.now()
    const timer = setTimeout(() => setNow(Date.now()), Math.max(0, Math.min(delay, 2_147_483_647)))
    return () => clearTimeout(timer)
  }, [preview.expiresAt])
  const expiry = new Intl.DateTimeFormat(i18n.language, {
    dateStyle: 'short',
    timeStyle: 'short',
  }).format(new Date(preview.expiresAt))
  async function copy() {
    try {
      await navigator.clipboard.writeText(preview.url)
      setCopied(true)
      setCopyError(false)
    } catch {
      setCopyError(true)
    }
  }
  return (
    <div className="public-preview-card" role="group" aria-label={t('publicPreviewTitle')}>
      <Globe size={18} aria-hidden="true" />
      <div className="public-preview-details">
        <strong>{t('publicPreviewTitle')}</strong>
        <a href={preview.url} target="_blank" rel="noopener noreferrer">
          {preview.url}
        </a>
        <time dateTime={preview.expiresAt}>
          {t(expired ? 'publicPreviewExpired' : 'publicPreviewExpires', { time: expiry })}
        </time>
        {copyError && <small role="alert">{t('publicPreviewCopyError')}</small>}
      </div>
      <button
        type="button"
        className="public-preview-copy"
        aria-label={t(copied ? 'publicPreviewCopied' : 'publicPreviewCopy')}
        onClick={() => void copy()}
      >
        {copied ? <Check size={16} aria-hidden="true" /> : <Copy size={16} aria-hidden="true" />}
      </button>
      <span className="sr-only" role="status">
        {copied ? t('publicPreviewCopied') : ''}
      </span>
    </div>
  )
}

export function PublicPreviewMessage({
  children,
  sources,
}: {
  children: string
  sources?: SourceRef[]
}) {
  const { body, previews } = extractPublicPreviews(children)
  return (
    <>
      {body && <Markdown sources={sources}>{body}</Markdown>}
      {previews.map((preview) => (
        <PublicPreviewCard key={preview.url} preview={preview} />
      ))}
    </>
  )
}
