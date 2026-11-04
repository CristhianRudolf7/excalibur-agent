import { useCallback, useLayoutEffect, useRef, useState, type JSX } from 'react'
import { Button } from '@/components/ui/button'
import { useViewerText } from './i18n'
import { mountScene } from './renderScene'
import type { Frame, Scene } from './types'

export interface SceneViewProps {
  scene: Scene | null
  loading?: boolean
  frame: Frame | null
  view: 'observer' | 'camera'
  camera?: string
  label: string
  className?: string
  onReady?: () => void
}
export function SceneView({
  scene,
  loading = false,
  frame,
  view,
  camera,
  label,
  className,
  onReady,
}: SceneViewProps): JSX.Element {
  const text = useViewerText()
  const recenter = useRef(0)
  const latestFrame = useRef(frame)
  const ready = useRef(onReady)
  useLayoutEffect(() => {
    latestFrame.current = frame
    ready.current = onReady
  }, [frame, onReady])
  const [unavailable, setUnavailable] = useState(false)
  const [meshError, setMeshError] = useState(false)
  const attach = useCallback(
    (node: HTMLDivElement | null) => {
      setUnavailable(false)
      setMeshError(false)
      if (!node || !scene) return
      try {
        return mountScene(node, {
          scene,
          view,
          camera,
          frame: () => latestFrame.current,
          ready: () => ready.current?.(),
          meshError: () => setMeshError(true),
          recenter: () => recenter.current,
        })
      } catch {
        setUnavailable(true)
      }
    },
    [scene, view, camera],
  )
  const message = unavailable
    ? text('webglUnavailable')
    : !scene
      ? text(loading ? 'loadingScene' : 'noScene')
      : !frame && scene.robot
        ? text('waitingPose')
        : view === 'camera' && (!frame || (camera && camera !== frame.camera.name))
          ? text('noCamera')
          : undefined
  return (
    <div
      role="img"
      aria-label={label}
      className={`relative h-full min-h-48 w-full overflow-hidden rounded-md bg-muted ${className ?? ''}`}
    >
      <div ref={attach} className="absolute inset-0" />
      {view === 'observer' && scene && !unavailable && (
        <Button
          className="absolute top-2 left-2 px-2 py-1 text-xs"
          onClick={() => {
            recenter.current += 1
          }}
        >
          {text('recenter')}
        </Button>
      )}
      {(message || meshError) && (
        <span
          role="status"
          className="absolute inset-x-0 bottom-0 bg-background/90 p-3 text-center text-sm text-muted-foreground"
        >
          {message ?? text('meshError')}
        </span>
      )}
    </div>
  )
}
