// @vitest-environment node
import { describe, expect, it } from 'vitest'
import { confidence, PoseGate, smoothPose, toPose, type Detection } from './pose'
const detection = (): Detection => ({
  landmarks: [
    Array.from({ length: 33 }, (_, i) => ({ x: i / 33, y: 0.5, z: -0.1, visibility: 0.9 })),
  ],
  worldLandmarks: [Array.from({ length: 33 }, () => ({ x: 1, y: 2, z: 3 }))],
  hands: [{ handedness: 'Left', landmarks: Array.from({ length: 21 }, () => [0.1, 0.2, 0.3]) }],
})
describe('pose contract and motion gate', () => {
  it('converts body, metric world points and hands without mirroring or subtracting neutral', () => {
    const pose = toPose(detection(), 123, 'left')!
    expect(pose.landmarks).toHaveLength(33)
    expect(pose.landmarks[0]).toEqual([0, 0.5, -0.1, 0.9])
    expect(pose.world?.[0]).toEqual([1, 2, 3])
    expect(pose.hands?.[0]?.handedness).toBe('Left')
    expect(pose.t).toBe(123)
    expect(pose.arm).toBe('left')
  })
  it('rejects missing, partial and nonfinite landmarks', () => {
    expect(toPose({ landmarks: [] }, 0, 'right')).toBeNull()
    const d = detection()
    d.landmarks[0]![0]!.x = NaN
    expect(toPose(d, 0, 'right')).toBeNull()
  })
  it('sends no faster than 30 Hz and never catches up in a burst', () => {
    const gate = new PoseGate(),
      pose = toPose(detection(), 0, 'right')!
    const sent = Array.from({ length: 1001 }, (_, t) => t).filter((t) =>
      gate.next({ ...pose, t }, true, true),
    )
    expect(sent.length).toBeLessThanOrEqual(30)
    expect(sent.every((t, i) => !i || t - sent[i - 1]! >= 1000 / 30)).toBe(true)
  })
  it('requires visibility for the selected arm and hips; blocks missing pose', () => {
    const pose = toPose(detection(), 0, 'right')!,
      gate = new PoseGate()
    pose.landmarks[16]![3] = 0.2
    expect(confidence(pose)).toBe(0.2)
    expect(gate.next(pose, true, true)).toBe(false)
    expect(gate.next(null, true, false)).toBe(false)
  })
  it('hold-to-move gates both initial and later sends without consuming the throttle', () => {
    const pose = toPose(detection(), 0, 'right')!,
      gate = new PoseGate()
    expect(gate.next(pose, false, true)).toBe(false)
    expect(gate.next(pose, true, true)).toBe(true)
    expect(gate.next({ ...pose, t: 100 }, false, true)).toBe(false)
    expect(gate.next({ ...pose, t: 100 }, false, false)).toBe(true)
  })
  it('smooths coordinates while preserving current confidence and calibration', () => {
    const pose = toPose(detection(), 0, 'left')!,
      previous = structuredClone(pose)
    previous.landmarks[0] = [1, 1, 1, 0.1]
    pose.calibration = { landmarks: structuredClone(pose.landmarks) }
    const result = smoothPose(pose, previous, 0.5)
    expect(result.landmarks[0]).toEqual([0.5, 0.75, 0.45, 0.9])
    expect(result.calibration).toBe(pose.calibration)
  })
})

it('throttles dispatch time even when asynchronous inference durations vary', () => {
  const pose = toPose(detection(), 10, 'right')!,
    gate = new PoseGate()
  expect(gate.next(pose, true, false, 100)).toBe(true)
  expect(gate.next({ ...pose, t: 110 }, true, false, 116)).toBe(false)
  expect(gate.next({ ...pose, t: 140 }, true, false, 150)).toBe(true)
})
