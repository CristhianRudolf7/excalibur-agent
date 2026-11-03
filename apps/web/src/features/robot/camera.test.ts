import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { rpc } from '@/lib/rpc/client'
import { cameraErrorKey, RobotCamera, sendFrame } from './camera'

vi.mock('@/lib/rpc/client', () => ({ rpc: { call: vi.fn() } }))

const upload = vi.fn()
const draw = vi.fn()
const track = { stop: vi.fn(), onended: null as (() => void) | null }
const stream = { getTracks: () => [track], getVideoTracks: () => [track] }
const getUserMedia = vi.fn()
let video: HTMLVideoElement
let canvas: HTMLCanvasElement
let camera: RobotCamera
const events = {
  sent: vi.fn<(at: number) => void>(),
  error: vi.fn<(key: string) => void>(),
  ended: vi.fn<() => void>(),
}

beforeEach(() => {
  vi.useFakeTimers()
  vi.clearAllMocks()
  vi.stubGlobal('fetch', upload)
  Object.defineProperty(navigator, 'mediaDevices', { configurable: true, value: { getUserMedia } })
  getUserMedia.mockResolvedValue(stream)
  upload.mockResolvedValue({ ok: true, json: async () => ({ digest: 'frame-digest' }) })
  vi.mocked(rpc.call).mockResolvedValue({ ok: true })
  vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue()
  vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue({
    drawImage: draw,
  } as unknown as ReturnType<HTMLCanvasElement['getContext']>)
  vi.spyOn(HTMLCanvasElement.prototype, 'toBlob').mockImplementation((callback) =>
    callback(new Blob(['jpeg'], { type: 'image/jpeg' })),
  )
  video = document.createElement('video')
  Object.defineProperties(video, {
    videoWidth: { configurable: true, value: 1920 },
    videoHeight: { configurable: true, value: 1080 },
    readyState: { configurable: true, value: 4 },
  })
  canvas = document.createElement('canvas')
  camera = new RobotCamera(events)
})
afterEach(() => {
  camera.stop()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  vi.useRealTimers()
})

describe('JPEG frame publication', () => {
  it('resizes the longest side, captures JPEG at 0.8, uploads then publishes the blob and dimensions', async () => {
    const signal = new AbortController().signal
    expect(await sendFrame(video, canvas, signal)).toBe(true)
    expect(canvas.width).toBe(1280)
    expect(canvas.height).toBe(720)
    expect(draw).toHaveBeenCalledWith(video, 0, 0, 1280, 720)
    expect(canvas.toBlob).toHaveBeenCalledWith(expect.any(Function), 'image/jpeg', 0.8)
    expect(upload).toHaveBeenCalledWith(
      '/blobs',
      expect.objectContaining({
        method: 'POST',
        headers: { 'Content-Type': 'image/jpeg' },
        body: expect.any(Blob),
      }),
    )
    expect(rpc.call).toHaveBeenCalledWith(
      'robot.frame',
      { blob: 'frame-digest', width: 1280, height: 720 },
      { signal },
    )
  })
  it('skips video that has not produced a frame and preserves small images', async () => {
    Object.defineProperty(video, 'readyState', { value: 1 })
    expect(await sendFrame(video, canvas, new AbortController().signal)).toBe(false)
    expect(upload).not.toHaveBeenCalled()
    Object.defineProperties(video, {
      readyState: { value: 4 },
      videoWidth: { value: 320 },
      videoHeight: { value: 480 },
    })
    await sendFrame(video, canvas, new AbortController().signal)
    expect([canvas.width, canvas.height]).toEqual([320, 480])
  })
  it('surfaces HTTP, missing reference and daemon rejection errors', async () => {
    upload.mockResolvedValueOnce({ ok: false, status: 503 })
    await expect(sendFrame(video, canvas, new AbortController().signal)).rejects.toThrow('503')
    upload.mockResolvedValueOnce({ ok: true, json: async () => ({}) })
    await expect(sendFrame(video, canvas, new AbortController().signal)).rejects.toThrow(
      'reference',
    )
    vi.mocked(rpc.call).mockResolvedValueOnce({ ok: false, error: 'No camera accepted' })
    await expect(sendFrame(video, canvas, new AbortController().signal)).rejects.toThrow(
      'No camera accepted',
    )
  })
})

describe('camera lifetime and frame loop', () => {
  it('starts the selected device, sends on the chosen cadence and stops every track and timer', async () => {
    await camera.start(video, 'webcam-2')
    await vi.advanceTimersByTimeAsync(0)
    expect(getUserMedia).toHaveBeenCalledWith({
      audio: false,
      video: { width: { ideal: 1280 }, deviceId: { exact: 'webcam-2' } },
    })
    expect(video.srcObject).toBe(stream)
    expect(events.sent).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(2000)
    expect(events.sent).toHaveBeenCalledTimes(2)
    camera.setInterval(5000)
    await vi.advanceTimersByTimeAsync(4999)
    expect(events.sent).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(1)
    expect(events.sent).toHaveBeenCalledTimes(3)
    camera.stop()
    expect(track.stop).toHaveBeenCalledOnce()
    expect(video.srcObject).toBeNull()
    await vi.advanceTimersByTimeAsync(10000)
    expect(events.sent).toHaveBeenCalledTimes(3)
  })
  it('does not overlap slow uploads and aborts publication when stopped', async () => {
    let finish!: (value: unknown) => void
    upload.mockReturnValueOnce(
      new Promise((resolve) => {
        finish = resolve
      }),
    )
    await camera.start(video, '')
    await vi.advanceTimersByTimeAsync(10000)
    expect(upload).toHaveBeenCalledOnce()
    camera.stop()
    expect(upload.mock.calls[0]?.[1].signal.aborted).toBe(true)
    finish({ ok: true, json: async () => ({ ref: 'late-frame' }) })
    await vi.advanceTimersByTimeAsync(0)
    expect(rpc.call).not.toHaveBeenCalled()
    expect(events.sent).not.toHaveBeenCalled()
    expect(events.error).not.toHaveBeenCalled()
  })
  it('releases media if permission arrives after cancellation', async () => {
    let permit!: (value: unknown) => void
    getUserMedia.mockReturnValueOnce(
      new Promise((resolve) => {
        permit = resolve
      }),
    )
    const starting = camera.start(video, '')
    camera.stop()
    permit(stream)
    expect(await starting).toBe(false)
    expect(track.stop).toHaveBeenCalledOnce()
    expect(upload).not.toHaveBeenCalled()
  })
  it('retries a failed upload at the next interval and reports track termination', async () => {
    upload.mockRejectedValueOnce(new Error('offline'))
    await camera.start(video, '')
    await vi.advanceTimersByTimeAsync(0)
    expect(events.error).toHaveBeenCalledWith('robotFrameFailure')
    await vi.advanceTimersByTimeAsync(2000)
    expect(events.sent).toHaveBeenCalledOnce()
    track.onended?.()
    expect(events.ended).toHaveBeenCalledOnce()
    expect(video.srcObject).toBeNull()
  })
  it('releases media if video playback fails', async () => {
    vi.mocked(video.play).mockRejectedValueOnce(new Error('playback failed'))
    await expect(camera.start(video, '')).rejects.toThrow('playback failed')
    expect(track.stop).toHaveBeenCalledOnce()
    expect(video.srcObject).toBeNull()
  })
  it('maps browser permission and device errors to friendly messages', () => {
    for (const [name, key] of [
      ['NotAllowedError', 'robotCameraPermission'],
      ['NotFoundError', 'robotCameraMissing'],
      ['NotReadableError', 'robotCameraBusy'],
      ['NotSupportedError', 'robotCameraUnsupported'],
      ['UnknownError', 'robotCameraFailure'],
    ])
      expect(cameraErrorKey(new DOMException('Camera error', name))).toBe(key)
  })
})
