import { describe, expect, it } from 'vitest'
import * as THREE from 'three'
import { applyPose, geomMesh, primitiveGeometry } from './geometry'
import type { SceneGeom } from './types'

it('applies a world z-up pose and converts wxyz to xyzw including a flat-array offset', () => {
  const body = new THREE.Group()
  const q = Math.SQRT1_2
  applyPose(body, [9, 9, 9, 1, 0, 0, 0, 1, 2, 3, q, 0, 0, q], 7)
  expect(body.position.toArray()).toEqual([1, 2, 3])
  expect(body.quaternion.toArray()).toEqual([0, 0, q, q])
  const rotated = new THREE.Vector3(1, 0, 0).applyQuaternion(body.quaternion)
  expect(rotated.x).toBeCloseTo(0)
  expect(rotated.y).toBeCloseTo(1)
  expect(rotated.z).toBe(0)
  applyPose(body, [NaN, 0, 0, 1, 0, 0, 0])
  expect(body.position.toArray()).toEqual([1, 2, 3])
})

describe('MuJoCo primitive dimensions', () => {
  it.each([
    ['box', [1, 2, 3], [2, 4, 6]],
    ['sphere', [0.5], [1, 1, 1]],
    ['cylinder', [0.5, 2], [1, 1, 4]],
    ['capsule', [0.5, 2], [1, 1, 5]],
    ['plane', [2, 3], [4, 6, 0]],
    ['plane', [0, 0], [12, 12, 0]],
  ] as [SceneGeom['type'], number[], number[]][])(
    'converts %s half-sizes and aligns the long axis with z',
    (type, size, expected) => {
      const geometry = primitiveGeometry({ type, size }, 12)!
      geometry.computeBoundingBox()
      const dimensions = geometry.boundingBox!.getSize(new THREE.Vector3()).toArray()
      dimensions.forEach((dimension, axis) => expect(dimension).toBeCloseTo(expected[axis]!, 4))
      geometry.dispose()
    },
  )
})

it('composes the geom local rotation with its primitive axis and preserves alpha/scale', () => {
  const geom: SceneGeom = {
    type: 'cylinder',
    size: [0.5, 1],
    pos: [1, 2, 3],
    quat: [1, 0, 0, 0],
    rgba: [1, 0, 0, 0.4],
    scale: [2, 3, 4],
  }
  const mesh = geomMesh(geom, primitiveGeometry(geom)!, false)
  expect(mesh.position.toArray()).toEqual([1, 2, 3])
  expect(mesh.quaternion.toArray()).toEqual([0, 0, 0, 1])
  expect(mesh.scale.toArray()).toEqual([2, 3, 4])
  expect(mesh.material.opacity).toBe(0.4)
  expect(mesh.material.transparent).toBe(true)
  expect(mesh.castShadow).toBe(false)
  mesh.geometry.dispose()
  mesh.material.dispose()
})
