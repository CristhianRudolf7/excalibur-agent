import { PanelSkeleton } from '@/features/viewer/PanelSkeleton'
import { useLoadingState } from '@/features/work/loading'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { FileText, Search, UploadCloud } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { useViewerStore } from '@/features/viewer/store'
import { libraryCall, libraryEnabled, isIngesting, originalUrl } from './api'
import { AttachmentChips, DocumentStatus } from './Attachments'
import { useAttachments, useDocumentPolling } from './use-attachments'
import { useLibraryStore } from './store'
import { seedDocuments } from './seed'
import type { DocumentInfo } from './types'
import './library.css'

export function Library() {
  const { t, i18n } = useTranslation()
  const [query, setQuery] = useState('')
  const [ids, setIds] = useState<string[]>([])
  const [cursor, setCursor] = useState('')
  const [loading, setLoading] = useState(libraryEnabled)
  const loadingState = useLoadingState(loading)
  const [paging, setPaging] = useState(false)
  const pageLoading = useLoadingState(paging)
  const [demo, setDemo] = useState(false)
  const [error, setError] = useState('')
  const [deleting, setDeleting] = useState<DocumentInfo | null>(null)
  const [busy, setBusy] = useState(false)
  const [revision, setRevision] = useState(0)
  const documents = useLibraryStore((state) => state.documents)
  const attachments = useAttachments((document) => {
    setIds((previous) => Array.from(new Set([document.id, ...previous])))
    setDemo(false)
  })
  useEffect(() => {
    if (!libraryEnabled) return
    const controller = new AbortController()
    const timer = setTimeout(() => {
      setLoading(true)
      void libraryCall('library.list', { query, limit: 50 }, { signal: controller.signal })
        .then((result) => {
          if (controller.signal.aborted) return
          result.documents.forEach(useLibraryStore.getState().update)
          setIds(result.documents.map((document) => document.id))
          setCursor(result.next_cursor)
          setDemo(false)
          setError('')
        })
        .catch(() => {
          if (!controller.signal.aborted) {
            setDemo(true)
            setIds([])
            setCursor('')
          }
        })
        .finally(() => {
          if (!controller.signal.aborted) setLoading(false)
        })
    }, 150)
    return () => {
      clearTimeout(timer)
      controller.abort()
    }
  }, [query, revision])
  const uploaded = attachments.items.flatMap((item) => (item.document ? [item.document.id] : []))
  const allIDs = Array.from(new Set([...uploaded, ...ids]))
  const rows = demo ? seedDocuments : allIDs.flatMap((id) => (documents[id] ? [documents[id]] : []))
  const visible = rows.filter((document) =>
    `${document.title} ${document.source_name}`.toLowerCase().includes(query.toLowerCase()),
  )
  useDocumentPolling(
    demo
      ? []
      : rows
          .filter((document) => isIngesting(document) && !uploaded.includes(document.id))
          .map((document) => document.id),
  )
  const action = async (operation: () => Promise<void>) => {
    setBusy(true)
    setError('')
    try {
      await operation()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t('library_uploadFailed'))
    } finally {
      setBusy(false)
    }
  }
  if (!libraryEnabled) return null
  return (
    <div className="library-panel">
      <header className="library-heading">
        <span className="panel-eyebrow">{t('libraryDocument')}</span>
        <h2>{t('library')}</h2>
      </header>
      <div
        className="library-drop"
        onDragOver={(event) => event.preventDefault()}
        onDrop={(event) => {
          event.preventDefault()
          attachments.add(event.dataTransfer.files)
        }}
      >
        <UploadCloud aria-hidden="true" />
        <label>
          {t('libraryDrop')}
          <input
            aria-label={t('libraryUpload')}
            type="file"
            multiple
            onChange={(event) => {
              if (event.target.files) attachments.add(event.target.files)
              event.target.value = ''
            }}
          />
        </label>
      </div>
      <AttachmentChips items={attachments.items} onRemove={attachments.remove} />
      <div className="library-search">
        <Search aria-hidden="true" />
        <input
          type="search"
          aria-label={t('librarySearch')}
          placeholder={t('librarySearch')}
          value={query}
          onChange={(event) => setQuery(event.target.value)}
        />
      </div>
      {demo && <p role="status">{t('libraryDemo')}</p>}
      {error && <p role="alert">{error}</p>}
      {loadingState.visible && <PanelSkeleton label={t('libraryLoading')} shape="library" />}
      {!loadingState.holding && !visible.length && <p>{t('libraryEmpty')}</p>}
      <ul className="library-list">
        {(!loadingState.holding ? visible : []).map((document) => (
          <li key={document.id}>
            <div className="library-document-title">
              <FileText aria-hidden="true" />
              <strong>{document.title || document.source_name}</strong>
            </div>
            <DocumentStatus document={document} />
            {document.status === 'failed' && (
              <small>
                {document.failure_stage}: {document.error}
              </small>
            )}
            <small>
              {t('libraryPages', { count: document.page_count })} ·{' '}
              <time dateTime={document.uploaded_at}>
                {new Intl.DateTimeFormat(i18n.language, { dateStyle: 'medium' }).format(
                  new Date(document.uploaded_at),
                )}
              </time>
            </small>
            <div className="library-actions">
              <Button onClick={() => useViewerStore.getState().open({ documentId: document.id })}>
                {t('libraryOpen')}
              </Button>
              {!demo && (
                <a href={originalUrl(document)} download={document.source_name}>
                  {t('libraryDownload')}
                </a>
              )}
              {document.status === 'failed' && (
                <Button
                  disabled={busy || demo}
                  onClick={() =>
                    void action(async () => {
                      const result = await libraryCall('library.ingest', {
                        blob_digest: document.blob_digest,
                        name: document.source_name,
                        mime: document.mime,
                      })
                      useLibraryStore.getState().update(result.document)
                      setRevision((value) => value + 1)
                    })
                  }
                >
                  {t('libraryRetry')}
                </Button>
              )}
              <Button disabled={busy || demo} onClick={() => setDeleting(document)}>
                {t('libraryDelete')}
              </Button>
            </div>
          </li>
        ))}
      </ul>
      {pageLoading.visible && <PanelSkeleton label={t('libraryLoading')} shape="library" />}
      {cursor && !demo && (
        <Button
          aria-busy={paging}
          disabled={busy}
          onClick={() =>
            void action(async () => {
              setPaging(true)
              try {
                const result = await libraryCall('library.list', { query, cursor, limit: 50 })
                result.documents.forEach(useLibraryStore.getState().update)
                setIds((previous) =>
                  Array.from(
                    new Set([...previous, ...result.documents.map((document) => document.id)]),
                  ),
                )
                setCursor(result.next_cursor)
              } finally {
                setPaging(false)
              }
            })
          }
        >
          {t('libraryMore')}
        </Button>
      )}
      {deleting && (
        <Dialog title="libraryDelete" onClose={() => setDeleting(null)}>
          <p>{deleting.title}</p>
          <p>{t('libraryBackup')}</p>
          <Button
            disabled={busy}
            onClick={() =>
              void action(async () => {
                const result = await libraryCall('library.delete', { id: deleting.id })
                if (!result.deleted) throw new Error(result.notice)
                useLibraryStore.getState().remove(deleting.id)
                setIds((previous) => previous.filter((id) => id !== deleting.id))
                for (const item of attachments.items)
                  if (item.document?.id === deleting.id) attachments.remove(item.key)
                setDeleting(null)
              })
            }
          >
            {t('libraryDelete')}
          </Button>
        </Dialog>
      )}
    </div>
  )
}
