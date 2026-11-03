import type { ComponentProps } from 'react'
import { Check, Circle, CircleAlert, Loader, Pause, Play, Square, WifiOff } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { useLabText } from './i18n'

export type RunState =
  'running' | 'paused' | 'stopped' | 'starting' | 'stopping' | 'done' | 'error' | 'noSignal'
export function RunStatus({
  state,
  surface = 'scene',
  progress,
}: {
  state: RunState
  surface?: 'scene' | 'experiment' | 'teleop'
  progress?: string
}) {
  const { text } = useLabText()
  const Icon = {
    running: Circle,
    paused: Pause,
    stopped: Square,
    starting: Loader,
    stopping: Loader,
    done: Check,
    error: CircleAlert,
    noSignal: WifiOff,
  }[state]
  const key =
    state === 'running'
      ? ({ scene: 'runRunning', experiment: 'runTraining', teleop: 'cameraOn' } as const)[surface]
      : state === 'stopped' && surface === 'teleop'
        ? 'cameraOff'
        : (
            {
              paused: 'runPaused',
              stopped: 'runStopped',
              starting: 'runStarting',
              stopping: 'runStopping',
              done: 'runDone',
              error: 'runError',
              noSignal: 'runNoSignal',
            } as const
          )[state]
  return (
    <span className="lab-run-status" data-state={state} role="status" aria-live="polite">
      <Icon size={14} aria-hidden="true" />
      <span>
        {text(key)}
        {progress && ` · ${progress}`}
      </span>
    </span>
  )
}
export function RunToggle({
  active,
  busy,
  label,
  stop = false,
  toggle = true,
  ...props
}: Omit<ComponentProps<typeof Button>, 'children'> & {
  active: boolean
  busy?: boolean
  label: string
  stop?: boolean
  toggle?: boolean
}) {
  const Icon = busy ? Loader : active ? (stop ? Square : Pause) : Play
  return (
    <Button
      variant={active && stop ? 'destructive' : 'primary'}
      {...props}
      disabled={busy || props.disabled}
      aria-pressed={toggle ? active : undefined}
      className={`lab-run-toggle ${props.className ?? ''}`}
    >
      <Icon size={16} aria-hidden="true" className={busy ? 'lab-run-spinner' : undefined} />
      {label}
    </Button>
  )
}
