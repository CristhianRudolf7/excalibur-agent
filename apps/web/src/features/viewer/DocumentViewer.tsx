import { WorkspaceImage } from './WorkspaceImage'
import { PanelSkeleton } from './PanelSkeleton'
import { useLoadingState } from '@/features/work/loading'
import { openWorkspaceFile } from '../files/open'
import { useState } from 'react'
import { FileText } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useViewerStore, type ViewerTarget } from './store'
import { useDocument, usePages, useChunk } from './api'
import { seedFormats } from './seeds'
import { Blocks } from './Blocks'
import { PdfView } from './PdfView'
import { SheetView } from './SheetView'
import { viewerEnabled } from './switches'
import { blobURL } from './positions'
import './i18n'
import './viewer.css'

export function DocumentViewer({ onClose }: { onClose?: () => void }) {
  const workspace = useViewerStore((s) => s.workspace)
  const target = useViewerStore((s) => s.target)
  const close = useViewerStore((s) => s.close)
  const open = useViewerStore((s) => s.open)
  const { t } = useTranslation('viewer')
  if (!viewerEnabled) return <p>{t('disabled')}</p>
  const finish = () => {
    close()
    onClose?.()
  }
  if (workspace)
    return (
      <section className="document-viewer workspace-preview" aria-label={t('title')}>
        <header className="viewer-header">
          <strong>{workspace.path.split('/').at(-1)}</strong>
        </header>
        {workspace.status === 'loading' ? (
          <PanelSkeleton label={t('opening')} />
        ) : workspace.status === 'image' ? (
          <WorkspaceImage key={workspace.url} url={workspace.url} path={workspace.path} />
        ) : (
          <div className="viewer-empty" role={workspace.status === 'error' ? 'alert' : 'status'}>
            <FileText className="panel-empty-icon" aria-hidden="true" />
            <h2>{t(workspace.status === 'error' ? 'openError' : 'unsupported')}</h2>
            <p>{t(workspace.status === 'error' ? 'openHelp' : 'unsupportedHelp')}</p>
            {workspace.status === 'error' && (
              <button onClick={() => void openWorkspaceFile(workspace.path)}>{t('retry')}</button>
            )}
          </div>
        )}
      </section>
    )
  if (!target)
    return (
      <div className="viewer-empty">
        <FileText className="panel-empty-icon" aria-hidden="true" />
        <h2>{t('title')}</h2>
        <p>{t('empty')}</p>
        <div className="viewer-samples">
          {seedFormats.map((format) => (
            <button
              key={format}
              onClick={() => open({ documentId: `demo-${format}`, chunkId: 'demo-citation' })}
            >
              {format.toUpperCase()}
            </button>
          ))}
        </div>
      </div>
    )
  return (
    <DocumentContent
      key={`${target.documentId}:${target.chunkId ?? ''}:${target.page ?? ''}`}
      target={target}
      onClose={finish}
    />
  )
}
function DocumentContent({ target, onClose }: { target: ViewerTarget; onClose: () => void }) {
  const { t } = useTranslation('viewer')
  const metadata = useDocument(target.documentId)
  const citation = useChunk(target.documentId, target.chunkId)
  const document = metadata.data?.value.document
  const chunk = citation.data?.value.chunk
  const [selection, setSelection] = useState<{ target: ViewerTarget; page: number } | null>(null)
  const selected = selection?.target === target ? selection.page : null
  const setSelected = (page: number) => setSelection({ target, page })
  const [notes, setNotes] = useState(false)
  const pdf = document?.format === 'pdf' || document?.mime === 'application/pdf'
  const [pdfCount, setPdfCount] = useState(0)
  const count = Math.max(1, pdfCount, document?.page_count ?? 1)
  const current = Math.max(1, Math.min(count, selected ?? chunk?.page ?? target.page ?? 1))
  const pages = usePages(target.chunkId && citation.isPending ? undefined : document, current)
  // Original PDF bytes remain viewable when extraction is pending or yields no blocks.
  const page =
    pages.data?.value.pages?.find((p) => p.number === current) ??
    (pdf ? { number: current, method: 'original', quality: 1, blocks: [] } : undefined)
  const slides = ['pptx', 'odp'].includes(document?.format ?? '')
  const sheets = ['xlsx', 'ods', 'csv'].includes(document?.format ?? '')
  const kind = slides ? 'slide' : sheets ? 'sheet' : pdf || document?.paginated ? 'page' : 'section'
  const demo = metadata.data?.demo || (!pdf && pages.data?.demo)
  const error = metadata.error || citation.error || (!pdf && pages.error)
  const loading = useLoadingState(
    !error &&
      (metadata.isPending || (!!target.chunkId && citation.isPending) || (!pdf && pages.isPending)),
  )
  const retry = () => {
    if (metadata.error) void metadata.refetch()
    if (citation.error) void citation.refetch()
    if (pages.error) void pages.refetch()
  }
  return (
    <section className="document-viewer" aria-label={t('title')}>
      <header className="viewer-header">
        <div className="viewer-title">
          <FileText aria-hidden="true" />
          <strong>{document?.title ?? t('title')}</strong>
          <span className="viewer-badge">{document?.format.toUpperCase()}</span>
        </div>
        <div className="viewer-toolbar">
          <span role="status">
            {document && t(document.status, { defaultValue: document.status })}
          </span>
          {demo && <span className="viewer-badge">{t('demo')}</span>}
          {document && blobURL(document.blob_digest) && (
            <a href={blobURL(document.blob_digest)} download={document.source_name}>
              {t('download')}
            </a>
          )}
          <button onClick={onClose}>{t('close')}</button>
        </div>
      </header>
      <nav className="viewer-toolbar" aria-label={t(kind)}>
        <button disabled={current <= 1} onClick={() => setSelected(current - 1)}>
          {t('previous')}
        </button>
        <label>
          {t(kind)}{' '}
          <input
            aria-label={t(kind)}
            type="number"
            min={1}
            max={count}
            value={current}
            onChange={(e) => {
              const n = Number(e.target.value)
              if (Number.isInteger(n)) setSelected(Math.max(1, Math.min(count, n)))
            }}
          />{' '}
          / {count}
        </label>
        <button disabled={current >= count} onClick={() => setSelected(current + 1)}>
          {t('next')}
        </button>
        {slides && (
          <button aria-pressed={notes} onClick={() => setNotes(!notes)}>
            {t('notes')}
          </button>
        )}
      </nav>
      {(slides || sheets) && (
        <div className="viewer-strip" role="tablist" aria-label={t(kind)}>
          {pages.data?.value.pages?.map((p) => (
            <button
              key={p.number}
              role="tab"
              aria-selected={current === p.number}
              onClick={() => setSelected(p.number)}
            >
              {p.title || `${t(kind)} ${p.number}`}
            </button>
          ))}
        </div>
      )}
      {error && (
        <div role="alert">
          {t('error')} <button onClick={retry}>{t('retry')}</button>
        </div>
      )}
      {document?.error && <p role="alert">{document.error}</p>}
      {pdf && document?.warnings?.length ? (
        <p className="viewer-notice" role="status">
          {t('extractionNotice')}
        </p>
      ) : (
        document?.warnings?.map((warning, i) => (
          <p className="viewer-notice" role="status" key={i}>
            {warning}
          </p>
        ))
      )}
      {loading.holding ? (
        loading.visible && (
          <PanelSkeleton
            label={t('loading')}
            shape={slides ? 'slides' : sheets ? 'grid' : 'paper'}
          />
        )
      ) : page ? (
        <div className="viewer-body">
          {pdf && document ? (
            <PdfView
              digest={document.blob_digest}
              onPageCount={setPdfCount}
              page={page}
              chunk={chunk}
              focusKey={target}
              demo={!!demo}
            />
          ) : sheets ? (
            <SheetView page={page} chunk={chunk} focusKey={target} />
          ) : (
            <article className={slides ? 'viewer-slide' : 'viewer-paper'}>
              <Blocks page={page} chunk={chunk} focusKey={target} notes={!slides || notes} />
            </article>
          )}
          {!pdf &&
            page.warnings?.map((warning, i) => (
              <p className="viewer-notice" role="status" key={i}>
                {warning}
              </p>
            ))}
        </div>
      ) : (
        !error && <p>{t('noPages')}</p>
      )}
    </section>
  )
}
