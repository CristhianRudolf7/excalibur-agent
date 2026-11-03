import type { Pose, Point, Landmark } from './contracts'
export interface Detection {
  landmarks: { x: number; y: number; z: number; visibility?: number }[][]
  worldLandmarks?: { x: number; y: number; z: number }[][]
  hands?: Pose['hands']
}
/** Mirror affects presentation only; anatomical handedness and coordinates stay intact. */
export function toPose(result: Detection, t: number, arm: 'left' | 'right'): Pose | null {
  const points = result.landmarks[0]
  if (
    points?.length !== 33 ||
    points.some((p) => ![p.x, p.y, p.z, p.visibility ?? 0].every(Number.isFinite))
  )
    return null
  return {
    landmarks: points.map((p) => [p.x, p.y, p.z, p.visibility ?? 0] as Landmark),
    world: result.worldLandmarks?.[0]?.map((p) => [p.x, p.y, p.z] as Point),
    hands: result.hands,
    t,
    arm,
  }
}
export function confidence(pose: Pose): number {
  const indices = pose.arm === 'left' ? [11, 13, 15, 23, 24] : [12, 14, 16, 23, 24]
  return Math.min(...indices.map((i) => pose.landmarks[i]?.[3] ?? 0))
}
export function smoothPose(pose: Pose, previous: Pose | undefined, smoothing: number): Pose {
  if (!previous) return pose
  const mix = (v: number, old: number) => v * (1 - smoothing) + old * smoothing
  return {
    ...pose,
    hands: pose.hands?.map((hand) => {
      const old = previous.hands?.find((h) => h.handedness === hand.handedness)
      return {
        ...hand,
        landmarks: hand.landmarks.map(
          (p, i) => p.map((v, j) => mix(v, old?.landmarks[i]?.[j] ?? v)) as Point,
        ),
      }
    }),
    landmarks: pose.landmarks.map(
      (p, i) => p.map((v, j) => (j === 3 ? v : mix(v, previous.landmarks[i]![j]!))) as Landmark,
    ),
    world: pose.world?.map(
      (p, i) => p.map((v, j) => mix(v, previous.world?.[i]?.[j] ?? v)) as Point,
    ),
  }
}
/** No queued frames: callers await one inference and drop work while busy. */
export class PoseGate {
  private last = -Infinity
  next(pose: Pose | null, held: boolean, holdRequired: boolean, now = pose?.t ?? 0): boolean {
    if (!pose || confidence(pose) < 0.65 || (holdRequired && !held) || now - this.last < 1000 / 30)
      return false
    this.last = now
    return true
  }
}
