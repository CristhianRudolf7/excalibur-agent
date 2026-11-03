import { describe, expect, it, vi, afterEach } from 'vitest'
import { inputThrottle, mergeMetrics, parameterFields } from './logic'
import {
  FrameBuffer,
  geomGeometry,
  geomMesh,
  inspectionCamera,
  interpolateBody,
  toYUp,
} from './scene'
import type { Frame, Geom, Scene } from './types'
const geom: Geom = {
  body: 0,
  type: 'box',
  size: [1, 2, 3],
  pos: [1, 2, 3],
  quat: [1, 0, 0, 0],
  rgba: [0.2, 0.4, 0.8, 1],
}
describe('MuJoCo primitives', () => {
  it('converts z-up and preserves half extents and local poses', () => {
    expect(toYUp([1, 2, 3])).toEqual([1, 3, -2])
    const mesh = geomMesh(geom)
    mesh.geometry.computeBoundingBox()
    expect(mesh.geometry.boundingBox!.max.toArray()).toEqual([1, 2, 3])
    expect(mesh.position.toArray()).toEqual(geom.pos)
    expect(mesh.quaternion.toArray()).toEqual([0, 0, 0, 1])
    mesh.geometry.dispose()
    mesh.material.dispose()
  })
  it.each(['sphere', 'ellipsoid', 'cylinder', 'capsule', 'plane'] as const)(
    'builds %s with finite dimensions',
    (type) => {
      const geometry = geomGeometry({ ...geom, type })
      geometry.computeBoundingBox()
      expect(geometry.boundingBox!.max.toArray().every(Number.isFinite)).toBe(true)
      if (type === 'cylinder') expect(geometry.boundingBox!.max.z).toBeCloseTo(2)
      if (type === 'capsule') expect(geometry.boundingBox!.max.z).toBeCloseTo(3)
      geometry.dispose()
    },
  )
})
describe('frame interpolation', () => {
  it('lerps position, slerps wxyz, clamps and takes shortest rotation', () => {
    const midpoint = interpolateBody([0, 0, 0, 1, 0, 0, 0], [2, 4, 6, 0, 0, 0, 1], 0.5)
    expect(midpoint.slice(0, 3)).toEqual([1, 2, 3])
    expect(midpoint[3]).toBeCloseTo(Math.SQRT1_2)
    expect(midpoint[6]).toBeCloseTo(Math.SQRT1_2)
    expect(interpolateBody([0, 0, 0, 1, 0, 0, 0], [1, 2, 3, -1, 0, 0, 0], 2)).toEqual([
      1, 2, 3, 1, 0, 0, 0,
    ])
  })
  it('bounds memory and snaps on episode reset', () => {
    const buffer = new FrameBuffer()
    const frame = { t: 0, episode: 0, bodies: [[0, 0, 0, 1, 0, 0, 0]] } as Frame
    buffer.push(frame, 0)
    buffer.push({ ...frame, t: 1, bodies: [[2, 0, 0, 1, 0, 0, 0]] }, 40)
    expect(buffer.sample(60)![0]![0]).toBe(1)
    expect(buffer.sample(500)![0]![0]).toBe(2)
    buffer.push({ ...frame, episode: 1 }, 80)
    expect(buffer.sample(81)![0]![0]).toBe(0)
  })
})
describe('parameters and metrics', () => {
  it('derives enum, integer bounds, defaults and required flags from schema', () => {
    expect(
      parameterFields({
        required: ['links'],
        properties: {
          links: { type: 'integer', minimum: 1, maximum: 3, default: 1 },
          task: { enum: ['reach', 'sort'] },
        },
      }),
    ).toEqual([
      { name: 'links', type: 'integer', minimum: 1, maximum: 3, default: 1, required: true },
      { name: 'task', enum: ['reach', 'sort'], required: false },
    ])
    expect(parameterFields({})).toEqual([])
  })
  it('merges replay and live points independently per run with a fixed bound', () => {
    const initial = [
      { run: 'a', step: 2, wall_s: 1, episode_return: 4 },
      { run: 'b', step: 2, wall_s: 2, episode_return: 8 },
    ]
    expect(mergeMetrics(initial, [{ run: 'a', step: 2, wall_s: 3, loss: 0.1 }])).toEqual([
      { run: 'a', step: 2, wall_s: 3, episode_return: 4, loss: 0.1 },
      initial[1],
    ])
    expect(mergeMetrics(initial, [{ run: 'a', step: 3, wall_s: 4 }], 2).length).toBe(2)
  })
})
describe('input throttling', () => {
  afterEach(() => vi.useRealTimers())
  it('caps calls at 30 Hz, coalesces input and sends the final neutral value', () => {
    vi.useFakeTimers()
    const send = vi.fn()
    const sender = inputThrottle(send)
    sender.push({ motor: 1 })
    sender.push({ motor: 0.5 })
    sender.push({ motor: 0 })
    expect(send).toHaveBeenCalledTimes(1)
    vi.advanceTimersByTime(34)
    expect(send).toHaveBeenLastCalledWith({ motor: 0 })
    expect(send).toHaveBeenCalledTimes(2)
    sender.push({ motor: 0 })
    vi.advanceTimersByTime(1000)
    expect(send).toHaveBeenCalledTimes(2)
    sender.push({ motor: 1 })
    sender.push({ motor: 0.2 })
    sender.cancel()
    vi.advanceTimersByTime(100)
    expect(send).toHaveBeenCalledTimes(3)
  })
})

it('renders a light neutral floor while preserving colored robot geoms', () => {
  const floor = geomMesh({ ...geom, type: 'plane' })
  expect(floor.material.color.getHexString()).toBe('e8edf3')
  expect(floor.material.roughness).toBe(1)
  expect(floor.castShadow).toBe(false)
  floor.geometry.dispose()
  floor.material.dispose()
})

it('centers rocket inspection on its launch height and preserves the arm camera', () => {
  const scene: Scene = {
    timestep: 0.02,
    bodies: [],
    geoms: [],
    cameras: [],
    actuators: [],
    scenario: 'rocket',
    params: { start_height: 5 },
    camera_default: { pos: [9, -12, 7], target: [0, 0, 2.5] },
  }
  const camera = inspectionCamera(scene)
  expect(camera.target).toEqual([0, 0, 4.8])
  expect(camera.pos[2]).toBeGreaterThan(camera.target[2])
  expect(inspectionCamera({ ...scene, scenario: 'arm' })).toEqual(scene.camera_default)
})
