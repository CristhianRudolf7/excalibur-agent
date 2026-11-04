import { useState } from 'react'

// Page tests use the viewer contract without requiring a GPU or the viewer worker's files.
export function SceneView({
  label,
  className,
}: {
  scene: unknown
  frame: unknown
  view: 'observer' | 'camera'
  label: string
  className?: string
}) {
  return (
    <div role="img" aria-label={label} className={className}>
      <canvas aria-label={label} />
    </div>
  )
}
export function useScene(_kind: 'env' | 'session', _id: string | undefined) {
  return { scene: null, loading: false, error: undefined }
}
export function useLiveFrame(_session: string | undefined) {
  return null
}
export function useReplay(_session: string | undefined, _attempt: number | undefined) {
  const [t, seek] = useState(0)
  const [playing, setPlaying] = useState(false)
  const [speed, setSpeed] = useState(1)
  return {
    frame: null,
    t,
    duration: 10,
    playing,
    loading: false,
    error: undefined,
    play: () => setPlaying(true),
    pause: () => setPlaying(false),
    seek,
    speed,
    setSpeed,
  }
}
export function ReplayControls(props: ReturnType<typeof useReplay> & { label?: string }) {
  return (
    <div aria-label={props.label}>
      <button onClick={props.playing ? props.pause : props.play}>
        {props.playing ? 'Pause' : 'Play'}
      </button>
      <input
        aria-label="Replay time"
        type="range"
        value={props.t}
        min={0}
        max={props.duration}
        onChange={(event) => props.seek(Number(event.target.value))}
      />
    </div>
  )
}
