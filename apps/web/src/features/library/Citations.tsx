import { createContext, useContext, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { Dialog } from '@/components/ui/dialog'
import { useViewerStore } from '@/features/viewer/store'
import type { SourceRef } from './types'
import './library.css'
import { sourceUrl } from './source-url'
import { libraryEnabled } from './api'

const SourceContext = createContext<SourceRef[]>([])
function openSource(source: SourceRef) {
  if (source.kind === 'document' && source.document_id) {
    useViewerStore
      .getState()
      .open({ documentId: source.document_id, chunkId: source.chunk_id, page: source.page })
  } else {
    const url = sourceUrl(source)
    if (url) window.open(url, '_blank', 'noopener,noreferrer')
  }
}
export function CitationMarker({ n, children }: { n: number; children?: ReactNode }) {
  const sources = useContext(SourceContext)
  const { t } = useTranslation()
  const source = sources.find((item) => item.n === n)
  if (!source) return <sup>{children}</sup>
  return (
    <button
      className="library-citation"
      aria-label={t('librarySource', { n })}
      title={source.verbatim || source.title}
      onClick={() => openSource(source)}
    >
      [{n}]
    </button>
  )
}
export function CitationProvider({
  sources,
  children,
}: {
  sources: SourceRef[]
  children: ReactNode
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const visible = sources.filter((source) => libraryEnabled || source.kind === 'web')
  const unique = visible.filter(
    (source, index) =>
      visible.findIndex((other) =>
        source.kind === 'web'
          ? other.url === source.url
          : other.document_id === source.document_id &&
            other.chunk_id === source.chunk_id &&
            other.page === source.page &&
            other.paragraph === source.paragraph,
      ) === index,
  )
  return (
    <SourceContext value={unique}>
      {children}
      {unique.length > 0 && (
        <button className="library-sources-button" onClick={() => setOpen(true)}>
          <span className="source-stack" aria-hidden="true">
            {unique.slice(0, 3).map((source, index) => (
              <SourceIcon source={source} key={index} />
            ))}
          </span>
          {t('librarySources', { count: unique.length })}
        </button>
      )}
      {open && (
        <Dialog
          className="sources-dialog"
          title={t('librarySources', { count: unique.length })}
          onClose={() => setOpen(false)}
        >
          <ol className="library-sources-list">
            {unique.map((source, index) => (
              <li
                key={source.url ?? `${source.document_id}:${source.chunk_id}:${index}`}
                value={index + 1}
              >
                <SourceIcon source={source} />
                {source.kind === 'web' ? (
                  <a href={sourceUrl(source)} target="_blank" rel="noopener noreferrer">
                    {source.title}
                  </a>
                ) : (
                  <button
                    onClick={() => {
                      openSource(source)
                      setOpen(false)
                    }}
                  >
                    {source.title}
                  </button>
                )}
                <small>
                  {t(source.kind === 'web' ? 'libraryWeb' : 'libraryDocument')}
                  {source.page_label || source.page
                    ? ` · ${t('libraryPage', { page: source.page_label || source.page })}`
                    : ''}
                  {source.paragraph
                    ? ` · ${t('libraryParagraph', { paragraph: source.paragraph })}`
                    : ''}
                  {source.section_path ? ` · ${source.section_path}` : ''}
                </small>
                {source.url && (
                  <small>
                    {source.domain ?? safeDomain(source.url)} · {source.url}
                  </small>
                )}
                {source.read_at && (
                  <time dateTime={source.read_at}>
                    {t('chatReadAt')} {new Date(source.read_at).toLocaleString()}
                  </time>
                )}
                {source.kind === 'document' && source.blob_digest && (
                  <a
                    href={`/blobs/${encodeURIComponent(source.blob_digest)}#page=${source.page ?? 1}`}
                    target="_blank"
                    rel="noopener noreferrer"
                  >
                    {t('chatOpenSource')}
                  </a>
                )}
                {source.excerpt && <blockquote>{source.excerpt}</blockquote>}
              </li>
            ))}
          </ol>
        </Dialog>
      )}
    </SourceContext>
  )
}

function safeDomain(url: string) {
  try {
    return new URL(url).hostname
  } catch {
    return ''
  }
}
function SourceIcon({ source }: { source: SourceRef }) {
  const domain = source.url ? safeDomain(source.url) : ''
  return (
    <span className="source-icon" aria-hidden="true">
      {domain ? (
        <img
          src={`https://${domain}/favicon.ico`}
          alt=""
          referrerPolicy="no-referrer"
          onError={(event) => {
            event.currentTarget.style.display = 'none'
          }}
        />
      ) : (
        '▤'
      )}
    </span>
  )
}
