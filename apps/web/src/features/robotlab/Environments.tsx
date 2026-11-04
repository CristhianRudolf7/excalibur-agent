import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Dialog } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { labCall } from './api'
import { useLabText } from './i18n'
import { SceneView, useScene } from './viewer'
import type { EnvironmentSummary } from './types'

export function Environments({
  onSelect,
  onDraft,
}: {
  onSelect(id: string): void
  onDraft(text: string): void
}) {
  const { text } = useLabText()
  const client = useQueryClient()
  const environments = useQuery({
    queryKey: ['robotlab', 'environments'],
    queryFn: () => labCall('robotlab.env.list', {}),
    refetchInterval: 5000,
  })
  const [deleting, setDeleting] = useState<EnvironmentSummary>()
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')
  return (
    <>
      <div className="lab-toolbar">
        <h2>{text('environments')}</h2>
        <Button onClick={() => onDraft(text('createDraft'))}>{text('new')}</Button>
      </div>
      {environments.isPending && <p role="status">{text('loading')}</p>}
      {environments.error && (
        <p role="alert">
          {environments.error.message}
          <Button onClick={() => void environments.refetch()}>{text('retry')}</Button>
        </p>
      )}
      {environments.data?.environments.length === 0 && <p>{text('noEnvironments')}</p>}
      <div className="lab-env-grid">
        {environments.data?.environments.map((environment) => (
          <article className="lab-card" key={environment.id}>
            <button
              className="lab-env-open"
              aria-label={environment.name}
              onClick={() => onSelect(environment.id)}
            >
              <EnvironmentThumbnail environment={environment} />
              <h3>{environment.name}</h3>
            </button>
            <p>
              {text(environment.kind)} · {environment.objects} {text('objects')}
            </p>
            <Button
              aria-label={`${text('delete')} ${environment.name}`}
              onClick={() => {
                setError('')
                setDeleting(environment)
              }}
            >
              {text('delete')}
            </Button>
          </article>
        ))}
      </div>
      {deleting && (
        <Dialog
          title={text('deleteTitle')}
          footer={false}
          onClose={() => {
            if (!pending) setDeleting(undefined)
          }}
          className="lab-dialog"
        >
          <p>{text('deleteConfirm', { name: deleting.name })}</p>
          {error && <p role="alert">{error}</p>}
          <div className="lab-actions">
            <Button disabled={pending} onClick={() => setDeleting(undefined)}>
              {text('cancel')}
            </Button>
            <Button
              disabled={pending}
              onClick={async () => {
                setPending(true)
                try {
                  await labCall('robotlab.env.delete', { id: deleting.id })
                  await client.invalidateQueries({ queryKey: ['robotlab', 'environments'] })
                  setDeleting(undefined)
                } catch (cause) {
                  setError(String(cause))
                } finally {
                  setPending(false)
                }
              }}
            >
              {text('delete')}
            </Button>
          </div>
        </Dialog>
      )}
    </>
  )
}
function EnvironmentThumbnail({ environment }: { environment: EnvironmentSummary }) {
  const { text } = useLabText()
  const image = useQuery({
    queryKey: ['robotlab', 'thumbnail', environment.id, environment.updated_at],
    queryFn: () => labCall('robotlab.env.thumbnail', { id: environment.id }),
    staleTime: Infinity,
    retry: false,
  })
  return image.data ? (
    <img
      className="lab-env-thumbnail"
      src={`data:image/png;base64,${image.data.png}`}
      alt={environment.name}
    />
  ) : (
    <div className="lab-env-thumbnail lab-placeholder" aria-label={environment.name}>
      {image.isPending ? text('loading') : text(environment.kind)}
    </div>
  )
}
export function EnvironmentDetail({
  id,
  onBack,
  onDraft,
}: {
  id: string
  onBack(): void
  onDraft(text: string): void
}) {
  const { text } = useLabText()
  const environment = useQuery({
    queryKey: ['robotlab', 'environment', id],
    queryFn: () => labCall('robotlab.env.get', { id }),
  })
  const { scene, loading, error } = useScene('env', id)
  return (
    <div className="lab-env-detail">
      <div className="lab-toolbar">
        <Button onClick={onBack}>{text('back')}</Button>
        <h2>{environment.data?.environment.name}</h2>
        <Button
          disabled={!environment.data}
          onClick={() =>
            onDraft(text('editDraft', { id, name: environment.data!.environment.name }))
          }
        >
          {text('edit')}
        </Button>
      </div>
      {(error || environment.error) && <p role="alert">{error || environment.error?.message}</p>}
      {(loading || environment.isPending) && <p role="status">{text('loading')}</p>}
      <SceneView
        scene={scene}
        frame={null}
        view="observer"
        label={environment.data?.environment.name ?? text('environment')}
        className="lab-env-scene"
      />
      <h3>{text('objects')}</h3>
      <ul className="lab-objects">
        {environment.data?.environment.objects.map((object) => (
          <li key={object.id}>
            <strong>{object.name}</strong>
            <span>
              {text('shape')}: {text(object.shape)}
            </span>
            <span>
              {text('size')}: {object.size.join(' × ')}
            </span>
            <span
              className="lab-color"
              style={{ backgroundColor: object.color }}
              role="img"
              aria-label={`${text('color')}: ${object.color}`}
            />
            <span>{text(object.dynamic ? 'dynamic' : 'fixed')}</span>
          </li>
        ))}
      </ul>
    </div>
  )
}
