import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Button } from '@/components/ui/button'
import { labCall } from '@/features/robotlab/api'
import { useLabText } from '@/features/robotlab/i18n'
import '@/features/robotlab/robotlab.css'
import type { LabSettings } from '@/features/robotlab/types'

export function RobotLabSettings() {
  const { text } = useLabText()
  const settings = useQuery({
    queryKey: ['robotlab', 'settings'],
    queryFn: () => labCall('robotlab.settings.get', {}),
  })
  return (
    <div className="settings-card">
      {settings.isPending && <p role="status">{text('loading')}</p>}
      {settings.error && (
        <p role="alert">
          {settings.error.message}
          <Button onClick={() => void settings.refetch()}>{text('retry')}</Button>
        </p>
      )}
      {settings.data && <SettingsForm initial={settings.data} />}
    </div>
  )
}
function SettingsForm({ initial }: { initial: LabSettings }) {
  const { text } = useLabText()
  const client = useQueryClient()
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')
  const [saved, setSaved] = useState(false)
  return (
    <form
      className="lab-settings-form"
      onSubmit={async (event) => {
        event.preventDefault()
        const data = new FormData(event.currentTarget)
        const max_attempts = Number(data.get('max_attempts'))
        const sim_speed = Number(data.get('sim_speed'))
        if (
          !Number.isInteger(max_attempts) ||
          max_attempts < 1 ||
          max_attempts > 100 ||
          !Number.isFinite(sim_speed) ||
          sim_speed < 0.25 ||
          sim_speed > 4
        ) {
          setError(text('settingsRange'))
          return
        }
        setPending(true)
        setError('')
        setSaved(false)
        try {
          const result = await labCall('robotlab.settings.set', { max_attempts, sim_speed })
          client.setQueryData(['robotlab', 'settings'], result)
          setSaved(true)
        } catch (cause) {
          setError(String(cause))
        } finally {
          setPending(false)
        }
      }}
    >
      <label>
        {text('maxAttempts')}
        <input
          type="number"
          name="max_attempts"
          min={1}
          max={100}
          step={1}
          required
          defaultValue={initial.max_attempts ?? 20}
          disabled={pending}
        />
      </label>
      <label>
        {text('simSpeed')}
        <input
          type="number"
          name="sim_speed"
          min={0.25}
          max={4}
          step="any"
          required
          defaultValue={initial.sim_speed ?? 1}
          disabled={pending}
        />
      </label>
      <p>{text('settingsRange')}</p>
      {error && <p role="alert">{error}</p>}
      {saved && <p role="status">{text('saved')}</p>}
      <Button type="submit" disabled={pending}>
        {text('save')}
      </Button>
    </form>
  )
}
