import * as THREE from 'three'
import type { BodyPose, Frame, Geom, Vec3, Scene } from './types'
/** Rotate the entire MuJoCo world around X; local body and geom poses stay in z-up. */
export const worldRotation = -Math.PI / 2
export function toYUp([x, y, z]: Vec3): Vec3 {
  return [x, z, -y]
}
export function geomGeometry(geom: Geom): THREE.BufferGeometry {
  const [x, y, z] = geom.size
  switch (geom.type) {
    case 'box':
      return new THREE.BoxGeometry(2 * x, 2 * y, 2 * z)
    case 'sphere':
      return new THREE.SphereGeometry(x, 24, 16)
    case 'ellipsoid':
      return new THREE.SphereGeometry(1, 24, 16).scale(x, y, z)
    case 'cylinder':
      return new THREE.CylinderGeometry(x, x, 2 * y, 24).rotateX(Math.PI / 2)
    case 'capsule':
      return new THREE.CapsuleGeometry(x, 2 * y, 8, 16).rotateX(Math.PI / 2)
    case 'plane':
      return new THREE.PlaneGeometry(x > 0 ? 2 * x : 30, y > 0 ? 2 * y : 30)
  }
}
export function geomMesh(geom: Geom) {
  const [r, g, b, a] = geom.rgba
  const mesh = new THREE.Mesh(
    geomGeometry(geom),
    new THREE.MeshStandardMaterial({
      color: geom.type === 'plane' ? new THREE.Color('#e8edf3') : new THREE.Color(r, g, b),
      opacity: a,
      transparent: a < 1,
      roughness: geom.type === 'plane' ? 1 : 0.65,
      metalness: geom.type === 'plane' ? 0 : 0.08,
      side: THREE.DoubleSide,
    }),
  )
  mesh.position.fromArray(geom.pos)
  const [w, x, y, z] = geom.quat
  mesh.quaternion.set(x, y, z, w)
  mesh.castShadow = geom.type !== 'plane'
  mesh.receiveShadow = true
  return mesh
}
export function interpolateBody(a: BodyPose, b: BodyPose, alpha: number): BodyPose {
  const t = Math.max(0, Math.min(1, alpha))
  const q = new THREE.Quaternion(a[4], a[5], a[6], a[3]).slerp(
    new THREE.Quaternion(b[4], b[5], b[6], b[3]),
    t,
  )
  return [
    a[0] + (b[0] - a[0]) * t,
    a[1] + (b[1] - a[1]) * t,
    a[2] + (b[2] - a[2]) * t,
    q.w,
    q.x,
    q.y,
    q.z,
  ]
}
/** Keep only two frames; interpolate with one frame of latency, never extrapolate. */
export class FrameBuffer {
  previous?: Frame
  current?: Frame
  arrival = 0
  interval = 1000 / 30
  push(frame: Frame, now: number) {
    this.previous =
      this.current && frame.t > this.current.t && frame.episode === this.current.episode
        ? this.current
        : frame
    if (this.current) this.interval = Math.max(16, Math.min(150, now - this.arrival))
    this.current = frame
    this.arrival = now
  }
  sample(now: number) {
    return this.current?.bodies.map((body, i) =>
      interpolateBody(this.previous?.bodies[i] ?? body, body, (now - this.arrival) / this.interval),
    )
  }
}

/** Bring overview cameras closer while preserving each world's direction and target. */
export function inspectionCamera(scene: Scene): { pos: Vec3; target: Vec3 } {
  const { pos, target: overviewTarget } = scene.camera_default
  const target: Vec3 =
    scene.scenario === 'rocket'
      ? [overviewTarget[0], overviewTarget[1], Number(scene.params.start_height ?? 5) - 0.2]
      : overviewTarget
  const scale: Record<string, number> = {
    cartpole: 0.45,
    ballplate: 0.85,
    quadruped: 0.7,
    drone: 0.42,
    rocket: 0.32,
  }
  const distance = scale[scene.scenario] ?? 1
  return {
    pos: pos.map((value, i) => target[i]! + (value - overviewTarget[i]!) * distance) as Vec3,
    target,
  }
}
