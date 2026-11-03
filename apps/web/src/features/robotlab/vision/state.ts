import type { VisionEvent } from '../types'
export type { VisionDecision, VisionEvent } from '../types'
export interface VisionState {
  session: string
  steps: VisionEvent[]
}
/** Merge partial phases per step; preserve image and decision through verification. */
export function visionReducer(state: VisionState, event: VisionEvent): VisionState {
  if (event.session !== state.session) return state
  const index = state.steps.findIndex((step) => step.step_id === event.step_id)
  if (index === -1) return { ...state, steps: [...state.steps, event].slice(-30) }
  const steps = state.steps.slice()
  const previous = steps[index]!
  const phases = ['see', 'think', 'act', 'verify', 'done', 'error']
  if (phases.indexOf(event.phase) < phases.indexOf(previous.phase)) return state
  steps[index] = {
    ...previous,
    ...event,
    decision: event.decision ? { ...previous.decision, ...event.decision } : previous.decision,
  }
  return { ...state, steps }
}
export function scaleBBox(
  bbox: [number, number, number, number],
  source: [number, number],
  target: [number, number],
) {
  if (
    source.some((v) => !Number.isFinite(v) || v <= 0) ||
    [...bbox, ...target].some((v) => !Number.isFinite(v))
  )
    return null
  const [x0, y0, x1, y1] = bbox
  return {
    x: (Math.max(0, x0) / source[0]) * target[0],
    y: (Math.max(0, y0) / source[1]) * target[1],
    width: (Math.max(0, Math.min(source[0], x1) - Math.max(0, x0)) / source[0]) * target[0],
    height: (Math.max(0, Math.min(source[1], y1) - Math.max(0, y0)) / source[1]) * target[1],
  }
}
export function isVisionEvent(data: unknown): data is Record<string, unknown> & VisionEvent {
  return (
    typeof data === 'object' &&
    data !== null &&
    'session' in data &&
    'step_id' in data &&
    'phase' in data &&
    typeof data.session === 'string' &&
    typeof data.step_id === 'string' &&
    ['see', 'think', 'act', 'verify', 'done', 'error'].includes(String(data.phase))
  )
}
