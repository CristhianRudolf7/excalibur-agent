// @vitest-environment node
import { expect, it } from 'vitest'
import { scaleBBox, visionReducer, type VisionEvent } from './state'
const event: VisionEvent = {
  session: 's',
  step_id: 'one',
  phase: 'see',
  image_png_b64: 'png',
  marks: [{ id: 1, bbox: [10, 20, 50, 60] }],
}
it('merges sparse phases, retaining snapshot and chosen object during verification', () => {
  let state = visionReducer({ session: 's', steps: [] }, event)
  state = visionReducer(state, {
    session: 's',
    step_id: 'one',
    phase: 'act',
    decision: {
      objects: [{ mark: 1, class: 'cube', reason: 'blue' }],
      action: { mark: 1, target: 'tray' },
      explanation: 'Sort by color',
    },
  })
  state = visionReducer(state, {
    session: 's',
    step_id: 'one',
    phase: 'verify',
    latency_ms: 120,
    model: 'fake',
  })
  expect(state.steps[0]?.image_png_b64).toBe('png')
  expect(state.steps[0]?.decision?.action?.mark).toBe(1)
  expect(state.steps[0]?.phase).toBe('verify')
  expect(visionReducer(state, event)).toBe(state)
  expect(visionReducer(state, { ...event, session: 'other' })).toBe(state)
})
it('keeps ordered bounded history and terminal errors', () => {
  let state = { session: 's', steps: [] as VisionEvent[] }
  for (let i = 0; i < 40; i++) state = visionReducer(state, { ...event, step_id: String(i) })
  expect(state.steps).toHaveLength(30)
  state = visionReducer(state, { ...event, step_id: '39', phase: 'error', detail: 'Unavailable' })
  expect(state.steps.at(-1)?.detail).toBe('Unavailable')
})
it('scales source pixel bboxes to responsive display dimensions and clips edges', () => {
  expect(scaleBBox([64, 48, 320, 240], [640, 480], [320, 240])).toEqual({
    x: 32,
    y: 24,
    width: 128,
    height: 96,
  })
  expect(scaleBBox([-10, -10, 700, 500], [640, 480], [640, 480])).toEqual({
    x: 0,
    y: 0,
    width: 640,
    height: 480,
  })
  expect(scaleBBox([0, 0, 1, 1], [0, 0], [100, 100])).toBeNull()
})
