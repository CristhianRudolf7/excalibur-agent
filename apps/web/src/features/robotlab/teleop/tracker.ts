import type { Detection } from './pose'
import type { Point } from './contracts'
import simdLoader from '@robotlab-mediapipe-wasm/vision_wasm_internal.js?url'
import simdBinary from '@robotlab-mediapipe-wasm/vision_wasm_internal.wasm?url'
import cpuLoader from '@robotlab-mediapipe-wasm/vision_wasm_nosimd_internal.js?url'
import cpuBinary from '@robotlab-mediapipe-wasm/vision_wasm_nosimd_internal.wasm?url'

export interface Tracker {
  detect(video: HTMLVideoElement, timestamp: number): Detection | Promise<Detection>
  close(): void
}
export type TrackerFactory = () => Promise<Tracker>
export const officialModelBase = 'https://storage.googleapis.com/mediapipe-models'
export async function cachedModel(url: string): Promise<Uint8Array> {
  let cache: Cache | undefined
  try {
    cache = await caches.open('excalibur-robotlab-models-v1')
  } catch {
    /* Storage is optional. */
  }
  const cached = await cache?.match(url)
  const response = cached ?? (await fetch(url, { signal: AbortSignal.timeout(60_000) }))
  if (!response.ok) throw new Error(`Model download failed (${response.status})`)
  if (!cached) {
    try {
      await cache?.put(url, response.clone())
    } catch {
      /* Quota does not prevent tracking. */
    }
  }
  return new Uint8Array(await response.arrayBuffer())
}
export async function createTracker(
  base = import.meta.env.EXCALIBUR_ROBOTLAB_MODEL_BASE || officialModelBase,
): Promise<Tracker> {
  const { FilesetResolver, PoseLandmarker, HandLandmarker } =
    await import('@mediapipe/tasks-vision')
  const simd = await FilesetResolver.isSimdSupported()
  const files = {
    wasmLoaderPath: simd ? simdLoader : cpuLoader,
    wasmBinaryPath: simd ? simdBinary : cpuBinary,
  }
  const [poseBytes, handBytes] = await Promise.all([
    cachedModel(`${base}/pose_landmarker/pose_landmarker_lite/float16/1/pose_landmarker_lite.task`),
    cachedModel(`${base}/hand_landmarker/hand_landmarker/float16/1/hand_landmarker.task`),
  ])
  async function load(delegate: 'GPU' | 'CPU') {
    const pose = await PoseLandmarker.createFromOptions(files, {
      baseOptions: { modelAssetBuffer: poseBytes, delegate },
      runningMode: 'VIDEO',
      numPoses: 1,
    })
    try {
      const hand = await HandLandmarker.createFromOptions(files, {
        baseOptions: { modelAssetBuffer: handBytes, delegate },
        runningMode: 'VIDEO',
        numHands: 2,
      })
      return {
        detect(video: HTMLVideoElement, t: number): Detection {
          const p = pose.detectForVideo(video, t)
          const h = hand.detectForVideo(video, t)
          return {
            ...p,
            hands: h.landmarks.map((points, i) => ({
              handedness: h.handedness[i]?.[0]?.categoryName === 'Left' ? 'Left' : 'Right',
              landmarks: points.map((p) => [p.x, p.y, p.z] as Point),
            })),
          }
        },
        close() {
          pose.close()
          hand.close()
        },
      }
    } catch (error) {
      pose.close()
      throw error
    }
  }
  try {
    return await load('GPU')
  } catch {
    return load('CPU')
  }
}
