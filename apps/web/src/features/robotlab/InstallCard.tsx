import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Button } from '@/components/ui/button'
import { labCall, robotlabEnabled, useRobotEvent } from './api'
import { useLabText } from './i18n'
import type { InstallEvent } from './types'

export function InstallCard() {
  const { text } = useLabText()
  const client = useQueryClient()
  const [installing, setInstalling] = useState(false)
  const [error, setError] = useState('')
  const [progress, setProgress] = useState<InstallEvent>()
  const status = useQuery({
    queryKey: ['robotlab', 'status'],
    queryFn: () => labCall('robotlab.status', {}),
    enabled: robotlabEnabled,
    retry: false,
    refetchInterval: installing ? 2000 : false,
  })
  useRobotEvent('robotlab.install', (event) => {
    setProgress(event)
    if (event.error || event.done) setInstalling(false)
    if (event.error) setError(event.error)
    if (event.done) void client.invalidateQueries({ queryKey: ['robotlab', 'status'] })
  })
  return (
    <article className="lab-card lab-install">
      <h2>{text('title')}</h2>
      <p>{text('description')}</p>
      {!robotlabEnabled || status.data?.enabled === false ? (
        <p>{text('unavailable')}</p>
      ) : (
        <Button
          disabled={installing || status.isPending}
          onClick={async () => {
            setInstalling(true)
            setError('')
            try {
              await labCall('robotlab.install', {}, { timeoutMs: 180_000 })
              await status.refetch()
            } catch (cause) {
              setError(String(cause))
              setInstalling(false)
            }
          }}
        >
          {text(installing ? 'installing' : 'install')}
        </Button>
      )}
      {installing && (
        <div role="status">
          <progress aria-label={text('installing')} max={100} value={progress?.percent} />
          <p>{progress?.message}</p>
        </div>
      )}
      {(error || status.error || status.data?.error) && (
        <p role="alert">{error || status.error?.message || status.data?.error}</p>
      )}
      {status.isError && <Button onClick={() => void status.refetch()}>{text('retry')}</Button>}
    </article>
  )
}
