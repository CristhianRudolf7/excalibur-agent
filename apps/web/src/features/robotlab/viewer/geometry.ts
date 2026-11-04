import * as THREE from 'three'
import type { SceneGeom } from './types'

/** MuJoCo stores wxyz; three stores xyzw. Coordinates remain world z-up. */
export function applyPose(object: THREE.Object3D, pose: readonly number[], offset = 0) {
  const tuple = pose.slice(offset, offset + 7)
  if (tuple.length !== 7 || !tuple.every(Number.isFinite)) return
  object.position.set(tuple[0]!, tuple[1]!, tuple[2]!)
  object.quaternion.set(tuple[4]!, tuple[5]!, tuple[6]!, tuple[3]!).normalize()
}

export function primitiveGeometry(geom: SceneGeom, planeExtent = 10): THREE.BufferGeometry | null {
  const size = geom.size ?? []
  const radius = size[0] ?? 0.05
  const halfHeight = size[1] ?? radius
  switch (geom.type) {
    case 'box':
      return new THREE.BoxGeometry(radius * 2, halfHeight * 2, (size[2] ?? radius) * 2)
    case 'sphere':
      return new THREE.SphereGeometry(radius, 24, 16)
    case 'cylinder': {
      const geometry = new THREE.CylinderGeometry(radius, radius, halfHeight * 2, 24)
      geometry.rotateX(Math.PI / 2)
      return geometry
    }
    case 'capsule': {
      // MuJoCo half-height excludes the hemispheres. Both primitives start along y.
      const geometry = new THREE.CapsuleGeometry(radius, halfHeight * 2, 8, 16)
      geometry.rotateX(Math.PI / 2)
      return geometry
    }
    case 'plane':
      // MuJoCo zero-sized planes are infinite. Cover at least the scene bounds.
      return new THREE.PlaneGeometry(
        radius > 0 ? radius * 2 : planeExtent,
        halfHeight > 0 ? halfHeight * 2 : planeExtent,
      )
    case 'mesh':
      return null
  }
}

export function geomMesh(geom: SceneGeom, geometry: THREE.BufferGeometry, shadows: boolean) {
  const [r = 0.65, g = 0.65, b = 0.65, alpha = 1] = geom.rgba ?? []
  const material = new THREE.MeshStandardMaterial({
    color: new THREE.Color(r, g, b),
    opacity: alpha,
    transparent: alpha < 1,
    depthWrite: alpha >= 1,
    roughness: 0.75,
    metalness: 0.05,
    side: geom.type === 'plane' ? THREE.DoubleSide : THREE.FrontSide,
  })
  const mesh = new THREE.Mesh(geometry, material)
  applyPose(mesh, [...(geom.pos ?? [0, 0, 0]), ...(geom.quat ?? [1, 0, 0, 0])])
  if (geom.scale) mesh.scale.fromArray(geom.scale)
  mesh.castShadow = shadows && geom.type !== 'plane'
  mesh.receiveShadow = shadows
  return mesh
}
