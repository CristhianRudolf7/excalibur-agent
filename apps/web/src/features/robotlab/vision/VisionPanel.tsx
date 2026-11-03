import { useEffect, useReducer, useRef, useState } from 'react'
import { RunStatus, RunToggle } from '../RunState'
import { useLabText } from '../i18n'
import { PanelSkeleton } from '@/features/viewer/PanelSkeleton'
import { robotlabCall, robotlabEvents } from '../teleop/api'
import { useRobotText } from '../teleop/i18n'
import { isVisionEvent, scaleBBox, visionReducer, type VisionEvent } from './state'
import '../teleop/capture.css'
function VisionImage({ step }: { step: VisionEvent }) {
  const [size, setSize] = useState<[number, number]>([0, 0])
  const text = useRobotText()
  return (
    <div className="robot-vision-image">
      <img
        src={`data:image/png;base64,${step.image_png_b64}`}
        alt={text('vision')}
        onLoad={(e) => setSize([e.currentTarget.naturalWidth, e.currentTarget.naturalHeight])}
      />
      {size[0] > 0 && (
        <svg viewBox={`0 0 ${size[0]} ${size[1]}`} role="img" aria-label={text('see')}>
          {step.marks?.map((mark) => {
            const rect = scaleBBox(mark.bbox, size, size)
            return (
              rect && (
                <g key={mark.id}>
                  <rect
                    {...rect}
                    fill="none"
                    className={
                      step.decision?.action?.mark === mark.id ? 'robot-mark-selected' : undefined
                    }
                    stroke="var(--primary-strong)"
                    strokeWidth={step.decision?.action?.mark === mark.id ? 6 : 2}
                    strokeDasharray={step.decision?.action?.mark === mark.id ? undefined : '6 3'}
                  />
                </g>
              )
            )
          })}
        </svg>
      )}
    </div>
  )
}
export function DecisionCard({ step }: { step: VisionEvent }) {
  const text = useRobotText(),
    decision = step.decision
  return (
    <article className="robot-vision-card">
      {decision?.objects.map((object) => (
        <p key={object.mark}>
          #{object.mark} <strong>{object.class}</strong> — {object.reason}
        </p>
      ))}
      {decision && (
        <>
          <p>
            {decision.action
              ? `#${decision.action.mark} → ${decision.action.target}`
              : text('none')}
          </p>
          <p>{decision.explanation}</p>
        </>
      )}
      {decision?.verified !== undefined && (
        <p>
          {decision.verify_note === 'simulation' ? text('simulationVerified') : text('verify')}:{' '}
          {decision.verified ? '✓' : '✗'}
        </p>
      )}
      {decision?.model_verified !== undefined && (
        <p>
          {text('modelOpinion')}: {decision.model_verified ? '✓' : '✗'}
        </p>
      )}
      <p>
        {step.model}
        {step.latency_ms !== undefined && ` · ${step.latency_ms} ms`}
      </p>
      {step.detail && <p role={step.phase === 'error' ? 'alert' : undefined}>{step.detail}</p>}
    </article>
  )
}
export function VisionPanel({ session }: { session: string }) {
  return <SessionVision key={session} session={session} />
}
function SessionVision({ session }: { session: string }) {
  const { text: labText } = useLabText()
  const text = useRobotText()
  const [state, dispatch] = useReducer(visionReducer, { session, steps: [] })
  const [instruction, setInstruction] = useState(''),
    [pending, setPending] = useState(false)
  const [offline, setOffline] = useState(false),
    [error, setError] = useState('')
  const alive = useRef(true)
  const enabled = import.meta.env.EXCALIBUR_WEB_ROBOTLAB_VISION !== '0'
  useEffect(() => {
    alive.current = true
    if (!enabled) return
    const unsubscribe = robotlabEvents(
      'robotlab.vision',
      session,
      (data) => {
        if (isVisionEvent(data)) {
          dispatch(data)
          setOffline(false)
        }
      },
      () => setOffline(true),
    )
    return () => {
      alive.current = false
      unsubscribe()
    }
  }, [session, enabled])
  async function run() {
    setPending(true)
    setError('')
    try {
      // The service publishes phases with the authoritative step_id; never invent one here.
      await robotlabCall('robotlab.vision.step', { session, instruction })
    } catch (e) {
      if (alive.current) setError(String(e))
    } finally {
      if (alive.current) setPending(false)
    }
  }
  const current = state.steps.at(-1)
  const busy = pending || (!!current && !['done', 'error'].includes(current.phase))
  if (!enabled) return <p>{text('visionDisabled')}</p>
  return (
    <section className="robot-capture" aria-label={text('vision')}>
      <h2>{text('vision')}</h2>
      <RunStatus
        state={
          error || current?.phase === 'error'
            ? 'error'
            : busy
              ? 'starting'
              : current?.phase === 'done'
                ? 'done'
                : 'stopped'
        }
      />
      {(offline || error) && (
        <p role="alert">
          {text('unavailable')}
          {error && `: ${error}`}
        </p>
      )}
      <ol className="robot-vision-timeline">
        {(['see', 'think', 'act', 'verify'] as const).map((phase) => (
          <li key={phase} aria-current={current?.phase === phase ? 'step' : undefined}>
            {text(phase)}
          </li>
        ))}
      </ol>
      {current?.image_png_b64 ? (
        <VisionImage key={`${current.step_id}-${current.image_png_b64.length}`} step={current} />
      ) : (
        <PanelSkeleton shape="canvas" label={text('vision')} />
      )}
      {current ? (
        <DecisionCard step={current} />
      ) : (
        <PanelSkeleton shape="paper" label={text('think')} />
      )}
      <form
        onSubmit={(e) => {
          e.preventDefault()
          if (!busy && instruction.trim()) void run()
        }}
      >
        <label htmlFor={`vision-${session}`}>{text('instruction')}</label>
        <textarea
          id={`vision-${session}`}
          value={instruction}
          maxLength={8000}
          onChange={(e) => setInstruction(e.target.value)}
        />
        <RunToggle
          type="submit"
          toggle={false}
          active={busy}
          busy={busy}
          disabled={!instruction.trim()}
          label={busy ? labText('runStarting') : text('run')}
        />
      </form>
      {state.steps.length > 1 && (
        <details>
          <summary>
            {text('history')} ({state.steps.length - 1})
          </summary>
          {state.steps
            .slice(0, -1)
            .reverse()
            .map((step) => (
              <details key={step.step_id}>
                <summary>
                  {step.step_id} · {step.phase}
                </summary>
                {step.image_png_b64 && <VisionImage step={step} />}
                <DecisionCard step={step} />
              </details>
            ))}
        </details>
      )}
    </section>
  )
}
export default VisionPanel
