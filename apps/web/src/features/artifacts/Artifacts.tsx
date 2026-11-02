import { useEffect, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { WorkspaceImage } from '@/features/viewer/WorkspaceImage'
import { ArtifactHistory } from './ArtifactHistory'
import { ArtifactActions } from './ArtifactActions'
import { PdfView } from '@/features/viewer/PdfView'
import '@/features/viewer/i18n'
import '@/features/viewer/viewer.css'
import { emptyArtifacts, groupArtifacts, useArtifacts } from './store'
import { loadArtifact } from './api'
import type { Artifact, TurnArtifact } from './types'
import './artifacts.css'

function useObjectURL(blob?: Blob) {
  const [resource, setResource] = useState<{ blob: Blob; url: string }>()
  useEffect(() => {
    if (!blob) return
    const url = URL.createObjectURL(blob)
    // The URL is an external browser resource with effect-scoped ownership.
    // eslint-disable-next-line react/set-state-in-effect
    setResource({ blob, url })
    return () => URL.revokeObjectURL(url)
  }, [blob])
  return resource?.blob === blob ? resource?.url : undefined
}
export function ArtifactPreview({ artifact, session }: { artifact: Artifact; session: string }) {
  const { t } = useTranslation()
  const query = useQuery({
    queryKey: ['artifact', artifact.id],
    queryFn: ({ signal }) => loadArtifact(artifact, signal),
    retry: false,
    staleTime: Infinity,
  })
  const url = useObjectURL(query.data?.blob)
  const [page, setPage] = useState(1)
  const [pageCount, setPageCount] = useState(1)
  return (
    <section className="artifact-preview" aria-label={artifact.title}>
      <h3>{artifact.title}</h3>
      {query.data && url && (
        <ArtifactActions artifact={artifact} data={query.data} url={url} session={session} />
      )}
      {query.isError ? (
        <p role="alert">
          {t('artifactError')}{' '}
          <button onClick={() => void query.refetch()}>{t('panelRetry')}</button>
        </p>
      ) : !url ? (
        <p role="status">{t('artifactLoading')}</p>
      ) : artifact.format === 'pdf' ? (
        <>
          <nav className="viewer-toolbar" aria-label={artifact.title}>
            <button disabled={page <= 1} onClick={() => setPage(page - 1)}>
              {t('artifactPrevious')}
            </button>
            <span>
              {page} / {pageCount}
            </span>
            <button disabled={page >= pageCount} onClick={() => setPage(page + 1)}>
              {t('artifactNext')}
            </button>
          </nav>
          <PdfView
            sourceURL={url}
            page={{ number: page, method: 'original', quality: 1, blocks: [] }}
            demo={false}
            onPageCount={setPageCount}
          />
        </>
      ) : (
        <WorkspaceImage key={url} url={url} path={artifact.title} interactive />
      )}
    </section>
  )
}
function ArtifactGroup({
  artifacts,
  session,
  selected,
  onSelect,
}: {
  artifacts: TurnArtifact[]
  session: string
  selected?: string
  onSelect: (id: string) => void
}) {
  const { t } = useTranslation()
  const artifact = artifacts.find((item) => item.id === selected) ?? artifacts[0]!
  return (
    <section className="artifact-group">
      {artifacts.length > 1 && (
        <nav className="artifact-details" aria-label={t('artifactDetails')}>
          {artifacts.map((item, index) => (
            <button
              key={item.id}
              aria-pressed={item.id === artifact.id}
              onClick={() => onSelect(item.id)}
            >
              {index === 0 ? `${t('artifactOverview')}: ${item.title}` : item.title}
            </button>
          ))}
        </nav>
      )}
      <ArtifactPreview key={artifact.id} artifact={artifact} session={session} />
    </section>
  )
}
export function Artifacts({ session }: { session: string }) {
  const { t } = useTranslation()
  const conversation = useArtifacts((state) => state.conversations[session]) ?? emptyArtifacts
  const groups = useMemo(
    () =>
      groupArtifacts(
        conversation.items.filter(
          (item) => !conversation.selectedTurn || item.turn_id === conversation.selectedTurn,
        ),
      ),
    [conversation],
  )
  const scope = JSON.stringify([session, conversation.selectedTurn])
  const [selection, setSelection] = useState<{ scope: string; id: string }>()
  const selected = selection?.scope === scope ? selection.id : undefined
  const group =
    groups.find((entry) => entry.artifacts.some((item) => item.id === selected)) ?? groups[0]
  const active = group?.artifacts.find((item) => item.id === selected) ?? group?.artifacts[0]
  const items = conversation.items.filter(
    (item) => !conversation.selectedTurn || item.turn_id === conversation.selectedTurn,
  )
  const select = (id: string) => setSelection({ scope, id })
  return (
    <div className="artifacts-view">
      <h2>{t('diagrams')}</h2>
      {conversation.selectedTurn && (
        <button
          className="artifact-show-all"
          onClick={() => useArtifacts.getState().showAll(session)}
        >
          {t('artifactShowAll')}
        </button>
      )}
      {!groups.length && <p>{t('artifactEmpty')}</p>}
      {!!items.length && <ArtifactHistory items={items} selected={active?.id} onSelect={select} />}
      {group && (
        <ArtifactGroup
          key={group.key}
          artifacts={group.artifacts}
          session={session}
          selected={active?.id}
          onSelect={select}
        />
      )}
    </div>
  )
}
