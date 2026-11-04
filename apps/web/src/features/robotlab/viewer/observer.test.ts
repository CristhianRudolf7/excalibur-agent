import { expect, it } from 'vitest'
import * as THREE from 'three'
import { geomMesh, primitiveGeometry } from './geometry'
import { isWall, observerOffset, wallOccludes } from './observer'
import type { SceneGeom } from './types'

it('recognizes unnamed world-body vertical slabs without fading robot links or floors', () => {
  expect(isWall({ type: 'box', size: [4, 0.05, 1.25] }, 0)).toBe(true)
  expect(isWall({ type: 'box', size: [0.1, 2, 0.5] }, 0)).toBe(true)
  expect(isWall({ type: 'box', size: [4, 2, 0.05] }, 0)).toBe(false)
  expect(isWall({ type: 'box', size: [0.5, 0.5, 1] }, 0)).toBe(false)
  expect(isWall({ type: 'box', size: [0.05, 2, 1] }, 1)).toBe(false)
})
it('detects only slabs intersecting the finite sightline, including rotated walls', () => {
  const geom: SceneGeom = {
    type: 'box',
    size: [3, 0.05, 1.25],
    pos: [0, 0, 1.25],
    quat: [Math.SQRT1_2, 0, 0, Math.SQRT1_2],
  }
  const wall = geomMesh(geom, primitiveGeometry(geom)!, true)
  const base = new THREE.Vector3(1, 0, 0.8)
  expect(wallOccludes(wall, new THREE.Vector3(-2, 0, 2), base)).toBe(true)
  expect(wallOccludes(wall, new THREE.Vector3(2, 0, 2), base)).toBe(false)
  expect(wallOccludes(wall, new THREE.Vector3(-2, 12, 2), base)).toBe(false)
  expect(wallOccludes(wall, new THREE.Vector3(-2, 0, 10), base)).toBe(false)
})
it('frames each robot behind-left at 35 degrees and rotates with its heading', () => {
  for (const robot of ['dog', 'drone', 'humanoid', 'arm']) {
    const offset = observerOffset(robot, new THREE.Quaternion())
    expect(offset.length()).toBeCloseTo(robot === 'arm' ? 2.2 : 3.5)
    expect(THREE.MathUtils.radToDeg(Math.asin(offset.z / offset.length()))).toBeCloseTo(35)
    expect(offset.x).toBeLessThan(0)
    expect(offset.y).toBeGreaterThan(0)
    const rotation = new THREE.Quaternion().setFromAxisAngle(
      new THREE.Vector3(0, 0, 1),
      Math.PI / 2,
    )
    expect(
      observerOffset(robot, rotation).distanceTo(offset.applyQuaternion(rotation)),
    ).toBeCloseTo(0)
  }
})
