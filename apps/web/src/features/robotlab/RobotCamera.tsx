import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { labCall } from './api'
import { useLabText } from './i18n'
import { SceneView } from './viewer'
import type { Frame, Scene } from './viewer/types'
import type { Session } from './types'

const robotCameras = {
  dog: ['head', 'rear'],
  arm: ['wrist', 'overhead'],
} as const

export function RobotCamera({
  session,
  scene,
  loading,
  frame,
  className,
}: {
  session: Session
  scene: Scene | null
  loading: boolean
  frame: Frame | null
  className?: string
}) {
  const { text } = useLabText()
  const cameras = robotCameras[session.robot]
  const [choice, setChoice] = useState<string>(cameras[0])
  const camera = `robot/${choice}`
  const recorded = frame?.camera.name === camera
  const live = session.status === 'live'
  const observation = useQuery({
    queryKey: ['robotlab', 'camera', session.id, session.attempt, camera],
    queryFn: ({ signal }) =>
      labCall('robotlab.robot.look', { session: session.id, camera }, { signal }),
    enabled: live && !recorded && choice !== cameras[0],
    refetchInterval: 2000,
    retry: false,
  })
  return (
    <div className={`lab-robot-camera ${className ?? ''}`}>
      {recorded || choice === cameras[0] ? (
        <SceneView
          scene={scene}
          loading={loading}
          frame={frame}
          view="camera"
          camera={camera}
          label={text('camera')}
        />
      ) : observation.data && live ? (
        <img
          className="lab-camera-image"
          src={`data:image/png;base64,${observation.data.png}`}
          alt={text('camera')}
        />
      ) : (
        <p role="status">{text(live ? 'loading' : 'cameraNotRecorded')}</p>
      )}
      {observation.error && live && <p role="alert">{observation.error.message}</p>}
      <label className="lab-camera-select">
        {text('cameraChoice')}
        <select value={choice} onChange={(event) => setChoice(event.target.value)}>
          {cameras.map((name) => (
            <option key={name} value={name}>
              {text(name)}
            </option>
          ))}
        </select>
      </label>
    </div>
  )
}
