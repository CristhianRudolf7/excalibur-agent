import { useCallback, useEffect } from 'react'
import { useSceneRun, useSceneRuns } from './sceneRun'
import { RunStatus, RunToggle } from './RunState'
import { useLabText } from './i18n'
export function SceneRunControls({
  session,
  onError,
  shortcut = false,
  indicatorOnly = false,
}: {
  session: string
  onError?: (error: unknown) => void
  shortcut?: boolean
  indicatorOnly?: boolean
}) {
  const { text } = useLabText()
  const { mode, previous, pending, control, state } = useSceneRun(session)
  const paused = mode === 'paused'
  const playback = useSceneRuns((state) => (state.session === session ? state.playback : undefined))
  const toggle = useCallback(() => {
    if (!pending) void control(paused ? previous : 'paused').catch(onError ?? (() => undefined))
  }, [control, pending, paused, previous, onError])
  useEffect(() => {
    if (!shortcut) return
    const key = (event: KeyboardEvent) => {
      if (
        event.code !== 'KeyP' ||
        event.repeat ||
        event.ctrlKey ||
        event.metaKey ||
        event.altKey ||
        (event.target as HTMLElement)?.closest(
          'input,textarea,select,button,[contenteditable="true"],.monaco-editor,dialog',
        )
      )
        return
      event.preventDefault()
      toggle()
    }
    window.addEventListener('keydown', key)
    return () => window.removeEventListener('keydown', key)
  }, [shortcut, toggle])
  return (
    <div className="lab-run-controls">
      <RunStatus state={state} />
      {!indicatorOnly && (
        <RunToggle
          active={!paused}
          busy={pending}
          label={text(paused ? 'resumeSimulation' : 'pauseSimulation')}
          title={`${text(paused ? 'resumeSimulation' : 'pauseSimulation')} (P)`}
          onClick={toggle}
        />
      )}
      {!indicatorOnly && playback && (
        <RunToggle
          active
          stop
          label={`${text('playing')} · ${text('stopPlayback')}`}
          onClick={() => void control('idle').catch(onError ?? (() => undefined))}
        />
      )}
    </div>
  )
}
