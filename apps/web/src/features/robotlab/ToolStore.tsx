import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { Button } from '@/components/ui/button'
import { PanelSkeleton } from '@/features/viewer/PanelSkeleton'
import { labCall, robotlabEnabled, useRobotEvent } from './api'
import { useLabText, type LabMessage } from './i18n'
import { Thumbnail } from './Thumbnail'
import type { InstallEvent } from './types'
import './robotlab.css'
export function ToolStore() {
  const { text } = useLabText()
  const [progress, setProgress] = useState<InstallEvent>()
  const [error, setError] = useState('')
  const [installing, setInstalling] = useState(false)
  const status = useQuery({
    queryKey: ['robotlab', 'status'],
    queryFn: () => labCall('robotlab.status', {}),
    enabled: robotlabEnabled,
    retry: false,
  })
  useRobotEvent('robotlab.install', (event) => {
    setProgress(event)
    if (event.error) {
      setError(event.error)
      setInstalling(false)
    }
    if (event.done) {
      setInstalling(false)
      void status.refetch()
    }
  })
  return (
    <div className="lab-store">
      <header>
        <h1>{text('tools')}</h1>
        <p>{text('description')}</p>
      </header>
      <article className="lab-card">
        <div className="lab-card-heading">
          <h2>{text('title')}</h2>
          <span className="lab-chip">MuJoCo</span>
        </div>
        <p>{text('description')}</p>
        <div className="lab-thumbnails">
          {['cartpole', 'ballplate', 'arm', 'quadruped', 'drone', 'rocket'].map((id) => (
            <figure className="lab-scenario-tile" key={id}>
              <Thumbnail id={id} />
              <figcaption>{text(id as LabMessage)}</figcaption>
            </figure>
          ))}
        </div>
        {!robotlabEnabled ? (
          <p>{text('unavailable')}</p>
        ) : status.isPending ? (
          <PanelSkeleton shape="boards" label={text('title')} />
        ) : status.data?.installed ? (
          <Button asChild variant="primary">
            <Link to="/lab">{text('open')}</Link>
          </Button>
        ) : (
          <>
            <p>Python {status.data?.requirements.python ?? '≥3.10'} · OSMesa</p>
            <Button
              variant="primary"
              disabled={installing || status.data?.enabled === false}
              onClick={() => {
                setInstalling(true)
                setError('')
                void labCall('robotlab.install', {}).catch((cause: unknown) => {
                  setError(String(cause))
                  setInstalling(false)
                })
              }}
            >
              {text('install')}
            </Button>
            {(installing || progress) && (
              <div className="lab-install" role="status">
                <progress aria-label={text('install')} max={100} value={progress?.percent} />
                <p>{progress?.message}</p>
              </div>
            )}
          </>
        )}
        {(error || status.error || status.data?.error) && (
          <p role="alert">{error || status.error?.message || status.data?.error}</p>
        )}
        {status.isError && <Button onClick={() => void status.refetch()}>{text('retry')}</Button>}
      </article>
      <div className="lab-future">
        {(['studio3d', 'studioData'] as const).map((title) => (
          <article key={title} className="lab-card" aria-disabled="true">
            <h2>{text(title)}</h2>
            <Button disabled>{text('soon')}</Button>
          </article>
        ))}
      </div>
    </div>
  )
}
