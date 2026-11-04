import * as THREE from 'three'
import type { SceneGeom } from './types'

/** World-body boxes use half-sizes; names are optional in the scene contract. */
export function isWall(geom: SceneGeom, bodyIndex: number) {
  const [x = 0, y = 0, z = 0] = geom.size ?? []
  return bodyIndex === 0 && geom.type === 'box' && z * 2 >= 1 && Math.min(x, y) <= 0.1
}

/** Test the finite camera-to-base segment in the wall's local coordinates. */
export function wallOccludes(wall: THREE.Mesh, camera: THREE.Vector3, base: THREE.Vector3) {
  wall.updateWorldMatrix(true, false)
  wall.geometry.computeBoundingBox()
  const inverse = wall.matrixWorld.clone().invert()
  const start = camera.clone().applyMatrix4(inverse)
  const end = base.clone().applyMatrix4(inverse)
  const direction = end.clone().sub(start)
  const length = direction.length()
  if (length < 0.001) return false
  const ray = new THREE.Ray(start, direction.divideScalar(length))
  const hit = ray.intersectBox(wall.geometry.boundingBox!, new THREE.Vector3())
  return hit !== null && hit.distanceTo(start) < length
}

export function observerOffset(robot: string | null, rotation: THREE.Quaternion) {
  const forward = new THREE.Vector3(1, 0, 0).applyQuaternion(rotation)
  const yaw = Math.atan2(forward.y, forward.x)
  const elevation = THREE.MathUtils.degToRad(35)
  const azimuth = yaw + (3 * Math.PI) / 4
  const distance = robot === 'arm' ? 2.2 : 3.5
  return new THREE.Vector3(
    Math.cos(azimuth) * Math.cos(elevation),
    Math.sin(azimuth) * Math.cos(elevation),
    Math.sin(elevation),
  ).multiplyScalar(distance)
}
