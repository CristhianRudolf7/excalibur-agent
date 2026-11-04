import type { JSX } from 'react'
import type { useReplay } from './hooks'
import { useViewerText } from './i18n'

export function ReplayControls(
  props: ReturnType<typeof useReplay> & { label?: string },
): JSX.Element {
  const text = useViewerText()
  const disabled = props.loading || props.duration <= 0 || !!props.error
  return (
    <div
      role="group"
      aria-label={props.label ?? text('replay')}
      className="flex flex-wrap items-center gap-3 text-sm text-foreground"
    >
      <button
        type="button"
        className="focus-visible:outline-ring rounded-md border border-border px-3 py-1 focus-visible:outline-2 disabled:opacity-50"
        disabled={disabled}
        onClick={props.playing ? props.pause : props.play}
      >
        {text(props.playing ? 'pause' : 'play')}
      </button>
      <input
        type="range"
        min={0}
        max={props.duration}
        step={0.05}
        value={props.t}
        aria-label={text('seek')}
        disabled={disabled}
        aria-valuetext={`${props.t.toFixed(2)} / ${props.duration.toFixed(2)} s`}
        onChange={(event) => props.seek(Number(event.target.value))}
        className="min-w-24 flex-1 accent-primary"
      />
      <span className="text-muted-foreground tabular-nums">
        {props.t.toFixed(2)} / {props.duration.toFixed(2)} s
      </span>
      <label className="flex items-center gap-2">
        <span>{text('speed')}</span>
        <select
          value={props.speed}
          onChange={(event) => props.setSpeed(Number(event.target.value))}
          className="focus-visible:outline-ring rounded-md border border-border bg-background px-2 py-1 focus-visible:outline-2"
        >
          {[...new Set([0.25, 0.5, 1, 2, 4, props.speed])]
            .sort((a, b) => a - b)
            .map((speed) => (
              <option key={speed} value={speed}>
                {speed}×
              </option>
            ))}
        </select>
      </label>
      {props.loading && <span role="status">{text('loading')}</span>}
      {props.error && <span role="alert">{props.error}</span>}
    </div>
  )
}
