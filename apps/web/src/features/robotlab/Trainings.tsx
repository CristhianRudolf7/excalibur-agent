import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { sendMessage } from '@/features/chat/live'
import { labCall } from './api'
import { useLabText } from './i18n'
import { orderedSessions } from './logic'
import type { RobotId } from './types'

export function Trainings({
  onSelect,
  onCreateEnvironment,
  onAgent,
}: {
  onSelect(session: string): void
  onCreateEnvironment(): void
  onAgent(): void
}) {
  const { text, language } = useLabText()
  const sessions = useQuery({
    queryKey: ['robotlab', 'sessions'],
    queryFn: () => labCall('robotlab.session.list', {}),
    refetchInterval: 5000,
  })
  const environments = useQuery({
    queryKey: ['robotlab', 'environments'],
    queryFn: () => labCall('robotlab.env.list', {}),
  })
  const [create, setCreate] = useState(false)
  const [waiting, setWaiting] = useState(false)
  return (
    <>
      <div className="lab-toolbar">
        <h2>{text('trainings')}</h2>
        <Button onClick={() => setCreate(true)}>{text('newSession')}</Button>
      </div>
      {waiting && <p role="status">{text('waiting')}</p>}
      {sessions.isPending && <p role="status">{text('loading')}</p>}
      {sessions.error && (
        <p role="alert">
          {sessions.error.message}
          <Button onClick={() => void sessions.refetch()}>{text('retry')}</Button>
        </p>
      )}
      {sessions.data?.sessions.length === 0 && <p>{text('noSessions')}</p>}
      <div className="lab-table-scroll">
        <table className="lab-table">
          <thead>
            <tr>
              {(['robot', 'environment', 'task', 'attempts', 'status', 'date'] as const).map(
                (key) => (
                  <th scope="col" key={key}>
                    {text(key)}
                  </th>
                ),
              )}
            </tr>
          </thead>
          <tbody>
            {orderedSessions(sessions.data?.sessions ?? []).map((session) => (
              <tr key={session.id}>
                <td>
                  <Button onClick={() => onSelect(session.id)} aria-label={session.name}>
                    {text(session.robot)}
                  </Button>
                </td>
                <td>
                  {environments.data?.environments.find((env) => env.id === session.environment)
                    ?.name ?? session.environment}
                </td>
                <td>{session.task}</td>
                <td>{session.attempts}</td>
                <td>
                  <span className="lab-badge">{text(session.status)}</span>
                </td>
                <td>
                  <time dateTime={session.created_at}>
                    {new Date(session.created_at).toLocaleString(language)}
                  </time>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {create && (
        <NewSessionDialog
          onClose={() => setCreate(false)}
          onCreateEnvironment={() => {
            setCreate(false)
            onCreateEnvironment()
          }}
          onSent={() => {
            setWaiting(true)
            onAgent()
            setCreate(false)
          }}
        />
      )}
    </>
  )
}
export function NewSessionDialog({
  onClose,
  onCreateEnvironment,
  onSent,
}: {
  onClose(): void
  onCreateEnvironment(): void
  onSent(): void
}) {
  const { text } = useLabText()
  const robots = useQuery({
    queryKey: ['robotlab', 'robots'],
    queryFn: () => labCall('robotlab.robots.list', {}),
  })
  const environments = useQuery({
    queryKey: ['robotlab', 'environments'],
    queryFn: () => labCall('robotlab.env.list', {}),
  })
  const [robot, setRobot] = useState<RobotId | ''>('')
  const [environment, setEnvironment] = useState('')
  const [task, setTask] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')
  const choices =
    environments.data?.environments.filter(
      (env) => !robot || env.kind === (robot === 'arm' ? 'table' : 'room'),
    ) ?? []
  return (
    <Dialog
      title={text('newSession')}
      className="lab-dialog"
      footer={false}
      onClose={() => {
        if (!pending) onClose()
      }}
    >
      <form
        onSubmit={async (event) => {
          event.preventDefault()
          if (!robot || !environment || !task.trim()) return
          setPending(true)
          setError('')
          const sent = await sendMessage(
            'excalibur',
            text('trainingMessage', {
              robot: text(robot),
              environment: choices.find((env) => env.id === environment)?.name ?? environment,
              task: task.trim(),
            }),
          )
          setPending(false)
          if (sent) onSent()
          else setError(text('sendError'))
        }}
      >
        <label>
          {text('robot')}
          <select
            autoFocus
            required
            value={robot}
            disabled={pending}
            onChange={(event) => {
              setRobot(event.target.value as RobotId)
              setEnvironment('')
            }}
          >
            <option value="">{text('choose')}</option>
            {robots.data?.robots.map((item) => (
              <option key={item.id} value={item.id}>
                {text(item.id)}
              </option>
            ))}
          </select>
        </label>
        <label>
          {text('environment')}
          <select
            required
            value={environment}
            disabled={pending}
            onChange={(event) => {
              if (event.target.value === '__new') onCreateEnvironment()
              else setEnvironment(event.target.value)
            }}
          >
            <option value="">{text('choose')}</option>
            {choices.map((env) => (
              <option key={env.id} value={env.id}>
                {env.name}
              </option>
            ))}
            <option value="__new">{text('createEnvironment')}</option>
          </select>
        </label>
        <label>
          {text('task')}
          <textarea
            required
            value={task}
            disabled={pending}
            onChange={(event) => setTask(event.target.value)}
          />
        </label>
        {(error || robots.error || environments.error) && (
          <p role="alert">{error || robots.error?.message || environments.error?.message}</p>
        )}
        <Button type="submit" disabled={pending || !robot || !environment || !task.trim()}>
          {text(pending ? 'sending' : 'send')}
        </Button>
      </form>
    </Dialog>
  )
}
