// @vitest-environment node
import { afterEach, expect, it, vi } from 'vitest'
import { cachedModel, createTracker } from './tracker'
const mocked = vi.hoisted(() => ({ poseCreate: vi.fn(), handCreate: vi.fn() }))
vi.mock('@mediapipe/tasks-vision', () => ({
  FilesetResolver: { isSimdSupported: async () => true },
  PoseLandmarker: { createFromOptions: mocked.poseCreate },
  HandLandmarker: { createFromOptions: mocked.handCreate },
}))
afterEach(() => {
  vi.unstubAllGlobals()
  vi.resetAllMocks()
})
it('uses the cached model without a network request', async () => {
  const fetcher = vi.fn()
  vi.stubGlobal('fetch', fetcher)
  vi.stubGlobal('caches', {
    open: async () => ({ match: async () => new Response(new Uint8Array([1, 2, 3])) }),
  })
  expect(await cachedModel('https://models.test/model')).toEqual(new Uint8Array([1, 2, 3]))
  expect(fetcher).not.toHaveBeenCalled()
})
it('continues if Cache Storage is unavailable and reports failed downloads', async () => {
  vi.stubGlobal('caches', {
    open: async () => {
      throw new Error('disabled')
    },
  })
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('', { status: 503 })))
  await expect(cachedModel('https://models.test/model')).rejects.toThrow('503')
})
it('falls back from GPU to CPU and releases a partially initialized pose model', async () => {
  const partial = { close: vi.fn() },
    pose = { close: vi.fn(), detectForVideo: vi.fn().mockReturnValue({ landmarks: [] }) },
    hand = {
      close: vi.fn(),
      detectForVideo: vi.fn().mockReturnValue({ landmarks: [], handedness: [] }),
    }
  mocked.poseCreate.mockResolvedValueOnce(partial).mockResolvedValueOnce(pose)
  mocked.handCreate.mockRejectedValueOnce(new Error('GPU unavailable')).mockResolvedValueOnce(hand)
  vi.stubGlobal('caches', {
    open: async () => ({ match: async () => new Response(new Uint8Array([1])) }),
  })
  const tracker = await createTracker('https://models.test')
  expect(partial.close).toHaveBeenCalledOnce()
  expect(mocked.poseCreate.mock.calls[0]?.[1].baseOptions.delegate).toBe('GPU')
  expect(mocked.poseCreate.mock.calls[1]?.[1].baseOptions.delegate).toBe('CPU')
  expect(await tracker.detect({} as HTMLVideoElement, 100)).toMatchObject({
    landmarks: [],
    hands: [],
  })
  tracker.close()
  expect(pose.close).toHaveBeenCalledOnce()
  expect(hand.close).toHaveBeenCalledOnce()
})
