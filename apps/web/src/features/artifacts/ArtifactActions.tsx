import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useWhiteboard, whiteboardEnabled } from '@/features/whiteboard/store'
import type { Artifact } from './types'
import type { loadArtifact } from './api'
import { addToWhiteboard, downloadBlob, exportPNG } from './actions'

export function ArtifactActions({
  artifact,
  data,
  url,
  session,
}: {
  artifact: Artifact
  data: Awaited<ReturnType<typeof loadArtifact>>
  url: string
  session: string
}) {
  const { t } = useTranslation()
  const board = useWhiteboard((state) => state.board)
  const [showSource, setShowSource] = useState(false)
  const [busy, setBusy] = useState(false)
  const [status, setStatus] = useState('')
  const source = data.source ?? (artifact.format === 'svg' ? data.content : undefined)
  const dialect = data.dialect ?? artifact.dialect ?? artifact.format.toUpperCase()
  const filename = artifact.title.replace(/[^\p{L}\p{N}_.-]+/gu, '-').slice(0, 100) || artifact.id
  const run = async (action: () => Promise<void>, success = '') => {
    setBusy(true)
    setStatus('')
    try {
      await action()
      setStatus(success)
    } catch {
      setStatus('artifactActionError')
    } finally {
      setBusy(false)
    }
  }
  return (
    <>
      <div className="artifact-actions">
        {source !== undefined && (
          <>
            <button aria-expanded={showSource} onClick={() => setShowSource(!showSource)}>
              {t('artifactSource')} · {dialect}
            </button>
            <button
              disabled={busy}
              onClick={() =>
                void run(() => navigator.clipboard.writeText(source), 'artifactCopied')
              }
            >
              {t('artifactCopy')}
            </button>
          </>
        )}
        <a href={url} download={`${filename}.${artifact.format}`}>
          {t('artifactDownload', { format: artifact.format.toUpperCase() })}
        </a>
        {data.svg && (
          <button
            disabled={busy}
            onClick={() =>
              void run(async () => downloadBlob(await exportPNG(data.blob), `${filename}.png`))
            }
          >
            {t('artifactDownload', { format: 'PNG' })}
          </button>
        )}
        {data.svg && whiteboardEnabled && board?.conversation_id === session && (
          <button
            disabled={busy}
            onClick={() => void run(() => addToWhiteboard(session, data.svg!), 'artifactAdded')}
          >
            {t('artifactWhiteboard')}
          </button>
        )}
      </div>
      {busy && <p role="status">{t('artifactExporting')}</p>}
      {status && <p role={status === 'artifactActionError' ? 'alert' : 'status'}>{t(status)}</p>}
      {showSource && source !== undefined && (
        <figure className="artifact-source">
          <figcaption>{dialect}</figcaption>
          <pre>
            <code>{source}</code>
          </pre>
        </figure>
      )}
    </>
  )
}
