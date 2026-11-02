import { useTranslation } from 'react-i18next'
import { X } from 'lucide-react'
import type { Attachment } from './use-attachments'
import type { DocumentInfo } from './types'
import './library.css'

export function DocumentStatus({ document }: { document: DocumentInfo }) {
  const { t } = useTranslation()
  const busy = document.status === 'pending' || document.status === 'processing'
  return (
    <span
      className={`library-status library-status-${document.status}`}
      title={[...(document.warnings ?? []), document.failure_stage, document.error]
        .filter(Boolean)
        .join('\n')}
    >
      {t(`library_${document.status}`)}
      {busy && (
        <progress
          aria-label={t(`library_${document.status}`)}
          value={document.total_pages > 0 ? document.progress_pages : undefined}
          max={Math.max(1, document.total_pages)}
        />
      )}
      {busy && document.total_pages > 0 && (
        <small>
          {document.progress_pages}/{document.total_pages}
        </small>
      )}
    </span>
  )
}
export function AttachmentChips({
  items,
  onRemove,
}: {
  items: Attachment[]
  onRemove: (key: string) => void
}) {
  const { t } = useTranslation()
  return (
    <div className="library-attachments" aria-live="polite">
      {items.map((item) => (
        <div className="library-chip" key={item.key}>
          <strong>{item.name}</strong>
          <small>
            {new Intl.NumberFormat(undefined, { maximumFractionDigits: 1 }).format(
              item.size / 1024,
            )}{' '}
            KiB
          </small>
          {item.document ? (
            <DocumentStatus document={item.document} />
          ) : item.phase === 'error' ? (
            <span role="alert">{t(item.error ?? 'library_uploadFailed')}</span>
          ) : (
            <span>
              {t(`library_${item.phase}`, { percent: item.percent })}
              {item.phase === 'uploading' && (
                <progress aria-label={t('libraryUpload')} value={item.percent} max={100} />
              )}
            </span>
          )}
          <button
            type="button"
            aria-label={`${t('libraryRemove')}: ${item.name}`}
            onClick={() => onRemove(item.key)}
          >
            <X size={14} />
          </button>
        </div>
      ))}
    </div>
  )
}
