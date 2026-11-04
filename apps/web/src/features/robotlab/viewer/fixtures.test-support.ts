import type { Frame, Scene } from './types'
export const sceneFixture: Scene = {
  version: 1,
  bodies: [
    { name: 'world', geoms: [] },
    { name: 'robot/base', geoms: [] },
  ],
  objects: [],
  cameras: [{ name: 'robot/head', fovy: 58 }],
  bounds: { min: [-2, -2, 0], max: [2, 2, 2] },
  robot: 'dog',
  environment: 'room',
}
export function frameFixture(seq = 0, overrides: Partial<Frame> = {}): Frame {
  return {
    session: 'session',
    attempt: 1,
    seq,
    t: seq / 20,
    poses: [0, 0, 0, 1, 0, 0, 0, seq / 20, 0, 0.5, 1, 0, 0, 0],
    camera: { name: 'robot/head', pose: [0, 0, 1, 1, 0, 0, 0] },
    held: null,
    status: 'running',
    ...overrides,
  }
}
