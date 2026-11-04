import { useEffect, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Button } from '@/components/ui/button'
import { labCall } from './api'
import {
  SceneView,
  useScene,
  useLiveFrame,
  useLatestRecordedFrame,
  useReplay,
  ReplayControls,
} from './viewer'
import { LabLayout } from './LabLayout'
import { Tabs } from './Tabs'
import { SessionAgent } from './SessionAgent'
import { RobotCamera } from './RobotCamera'
import { useLabText } from './i18n'
import { useLab } from './state'
import type { Session } from './types'

export function SessionView({
  id,
  mobile,
  onBack,
}: {
  id: string
  mobile: boolean
  onBack(): void
}) {
  const { text } = useLabText()
  const session = useQuery({
    queryKey: ['robotlab', 'session', id],
    queryFn: () => labCall('robotlab.session.get', { session: id }),
    refetchInterval: (query) => (query.state.data?.session.status === 'live' ? 3000 : false),
  })
  useEffect(() => {
    if (!mobile) useLab.getState().openAgent()
  }, [id, mobile])
  if (session.isPending) return <p role="status">{text('loading')}</p>
  if (session.error)
    return (
      <p role="alert">
        {session.error.message}
        <Button onClick={() => void session.refetch()}>{text('retry')}</Button>
      </p>
    )
  return (
    <SessionWorkspace key={id} session={session.data.session} mobile={mobile} onBack={onBack} />
  )
}
function SessionWorkspace({
  session,
  mobile,
  onBack,
}: {
  session: Session
  mobile: boolean
  onBack(): void
}) {
  const { text } = useLabText()
  const client = useQueryClient()
  const environment = useQuery({
    queryKey: ['robotlab', 'environment', session.environment],
    queryFn: () => labCall('robotlab.env.get', { id: session.environment }),
  })
  const [selected, setSelected] = useState<{ current: number; n: number }>()
  const [swapped, setSwapped] = useState(false)
  const request = useLab((state) => state.agentRequest)
  const [mobileSelection, setMobileSelection] = useState<{
    request: number
    view: 'observer' | 'camera' | 'agent'
  }>(() => ({
    request,
    view: useLab.getState().focusedSession === session.id ? 'agent' : 'observer',
  }))
  const mobileView = mobileSelection.request === request ? mobileSelection.view : 'agent'
  const setMobileView = (view: 'observer' | 'camera' | 'agent') =>
    setMobileSelection({ request, view })
  const [stopping, setStopping] = useState(false)
  const [error, setError] = useState('')
  const live = session.status === 'live'
  const attempt = live
    ? session.attempt
    : selected?.current === session.attempt
      ? selected.n
      : session.attempt
  const scene = useScene('session', session.id, live)
  const liveFrame = useLiveFrame(live ? session.id : undefined, live ? session.attempt : undefined)
  const replay = useReplay(live ? undefined : session.id, live ? undefined : attempt)
  const recorded = useLatestRecordedFrame(
    live && !liveFrame ? session.id : undefined,
    live && !liveFrame ? session.attempt : undefined,
  )
  const frame = live ? (liveFrame ?? recorded) : replay.frame
  const agent = (
    <SessionAgent session={session} attempt={attempt} time={live ? undefined : replay.t} />
  )
  const header = (
    <header className="lab-session-header">
      <div className="lab-toolbar">
        <Button onClick={onBack}>{text('back')}</Button>
        <h2>
          {text(session.robot)} · {environment.data?.environment?.name || session.environment}
        </h2>
        <span className="lab-badge">{text(session.status)}</span>
        {live && (
          <Button
            disabled={stopping}
            onClick={async () => {
              setStopping(true)
              setError('')
              try {
                const result = await labCall('robotlab.session.stop', {
                  session: session.id,
                  by: 'user',
                })
                client.setQueryData(['robotlab', 'session', session.id], result)
                await client.invalidateQueries({ queryKey: ['robotlab', 'sessions'] })
              } catch (cause) {
                setError(String(cause))
              } finally {
                setStopping(false)
              }
            }}
          >
            {text('stop')}
          </Button>
        )}
      </div>
      <p>
        {text('task')}: {session.task}
      </p>
      <p>
        {text('goal')}: <Goal goal={session.goal} />
      </p>
      <p>
        {text('attempt')} {attempt}/{session.max_attempts}
      </p>
      {(error || scene.error || replay.error) && (
        <p role="alert">{error || scene.error || replay.error}</p>
      )}
    </header>
  )
  const controls = (
    <div className="lab-replay">
      <label>
        {text('attempt')}{' '}
        <select
          aria-label={text('attempt')}
          value={attempt}
          disabled={live}
          onChange={(event) =>
            setSelected({ current: session.attempt, n: Number(event.target.value) })
          }
        >
          {session.attempts.map((item) => (
            <option key={item.n} value={item.n}>
              {item.n} ·{' '}
              {text(
                item.status === 'abandoned'
                  ? 'stopped'
                  : item.status === 'running'
                    ? 'live'
                    : item.status,
              )}
            </option>
          ))}
        </select>
      </label>
      {!live && <ReplayControls {...replay} label={text('replay')} />}
    </div>
  )
  if (mobile)
    return (
      <div className="lab-session">
        {header}
        <Tabs
          label={text('title')}
          values={['observer', 'camera', 'agent']}
          value={mobileView}
          onChange={setMobileView}
        />
        {mobileView === 'agent' ? (
          <div className="lab-mobile-agent">{agent}</div>
        ) : (
          <div className="lab-mobile-view">
            {mobileView === 'camera' ? (
              <RobotCamera
                session={session}
                scene={scene.scene}
                loading={scene.loading}
                frame={frame}
              />
            ) : (
              <SceneView
                scene={scene.scene}
                loading={scene.loading}
                frame={frame}
                view="observer"
                label={text('observer')}
              />
            )}
          </div>
        )}
        {controls}
      </div>
    )
  return (
    <LabLayout mobile={false} agentContent={agent}>
      <div className="lab-session">
        {header}
        <div className="lab-stage">
          <SceneView
            scene={scene.scene}
            loading={scene.loading}
            frame={frame}
            view="observer"
            label={text('observer')}
            className={swapped ? 'lab-camera' : 'lab-observer'}
          />
          <RobotCamera
            session={session}
            scene={scene.scene}
            loading={scene.loading}
            frame={frame}
            className={swapped ? 'lab-observer' : 'lab-camera'}
          />
          <Button className="lab-swap" onClick={() => setSwapped(!swapped)}>
            {text('swap')}
          </Button>
        </div>
        {controls}
      </div>
    </LabLayout>
  )
}
function Goal({ goal }: { goal: Record<string, unknown> }) {
  const { text } = useLabText()
  // Goal data belongs to the session and is displayed without interpreting success.
  const type = String(goal.type ?? '')
  if (type === 'all' && Array.isArray(goal.goals))
    return (
      <>
        {goal.goals.map((item, index) => (
          <span key={index}>
            <Goal goal={item as Record<string, unknown>} />
            {index < (goal.goals as unknown[]).length - 1 ? '; ' : ''}
          </span>
        ))}
      </>
    )
  const target = goal.target as
    { object?: string; point?: number[]; robot_start?: boolean } | undefined
  const destination =
    target?.object ?? target?.point?.join(', ') ?? (target?.robot_start ? text('robotStart') : '')
  const object = String(goal.object ?? '')
  if (type === 'object_near')
    return (
      <>{text('goalObjectNear', { object, target: destination, radius: Number(goal.radius) })}</>
    )
  if (type === 'robot_near')
    return <>{text('goalRobotNear', { target: destination, radius: Number(goal.radius) })}</>
  if (type === 'object_held') return <>{text('goalHeld', { object })}</>
  if (type === 'robot_on')
    return <>{text('goalRobotOn', { target: destination, feet: Number(goal.feet ?? 4) })}</>
  if (type === 'object_on') return <>{text('goalOn', { object, target: destination })}</>
  return <>{text('unknown')}</>
}
