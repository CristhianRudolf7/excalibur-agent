import { rpc } from '@/lib/rpc/client'
import type { RobotImageSize } from '@/lib/rpc/contract'

export function cameraErrorKey(error: unknown): string {
  const name = error && typeof error === 'object' && 'name' in error ? error.name : ''
  if (name === 'NotAllowedError' || name === 'SecurityError') return 'robotCameraPermission'
  if (name === 'NotFoundError' || name === 'OverconstrainedError') return 'robotCameraMissing'
  if (name === 'NotReadableError') return 'robotCameraBusy'
  if (name === 'NotSupportedError') return 'robotCameraUnsupported'
  return 'robotCameraFailure'
}

export async function sendFrame(
  video: HTMLVideoElement,
  canvas: HTMLCanvasElement,
  signal: AbortSignal,
) {
  if (video.readyState < 2 || !video.videoWidth || !video.videoHeight) return false
  const scale = Math.min(1, 1280 / Math.max(video.videoWidth, video.videoHeight))
  canvas.width = Math.max(1, Math.round(video.videoWidth * scale))
  canvas.height = Math.max(1, Math.round(video.videoHeight * scale))
  const context = canvas.getContext('2d')
  if (!context) throw new Error('Canvas unavailable')
  context.drawImage(video, 0, 0, canvas.width, canvas.height)
  const jpeg = await new Promise<Blob>((resolve, reject) =>
    canvas.toBlob(
      (blob) => (blob ? resolve(blob) : reject(new Error('JPEG capture failed'))),
      'image/jpeg',
      0.8,
    ),
  )
  signal.throwIfAborted()
  const response = await fetch('/blobs', {
    method: 'POST',
    headers: { 'Content-Type': 'image/jpeg' },
    body: jpeg,
    signal: AbortSignal.any([signal, AbortSignal.timeout(15_000)]),
  })
  if (!response.ok) throw new Error(`Frame upload: HTTP ${response.status}`)
  const uploaded: { ref?: string; digest?: string; blob_digest?: string } = await response.json()
  const blob = uploaded.ref ?? uploaded.digest ?? uploaded.blob_digest
  if (!blob) throw new Error('Missing frame blob reference')
  signal.throwIfAborted()
  const result = await rpc.call(
    'robot.frame',
    { blob, width: canvas.width, height: canvas.height },
    { signal },
  )
  if (!result.ok) throw new Error(result.error || 'Frame rejected')
  return true
}

/** Owns media and the non-overlapping frame loop, including pending permission requests. */
export class RobotCamera {
  private readonly events: {
    sent: (at: number, image: RobotImageSize) => void
    error: (key: string) => void
    ended: () => void
  }
  private generation = 0
  private stream?: MediaStream
  private timer?: ReturnType<typeof setInterval>
  private controller?: AbortController
  private interval = 2000
  private busy = false
  private video?: HTMLVideoElement
  private canvas?: HTMLCanvasElement

  constructor(events: RobotCamera['events']) {
    this.events = events
  }

  async start(video: HTMLVideoElement, device: string) {
    this.stop()
    const generation = this.generation
    if (!navigator.mediaDevices?.getUserMedia)
      throw new DOMException('Media unavailable', 'NotSupportedError')
    const stream = await navigator.mediaDevices.getUserMedia({
      audio: false,
      video: { width: { ideal: 1280 }, ...(device ? { deviceId: { exact: device } } : {}) },
    })
    if (generation !== this.generation) {
      stream.getTracks().forEach((track) => track.stop())
      return false
    }
    this.stream = stream
    this.video = video
    video.srcObject = stream
    try {
      await video.play()
    } catch (error) {
      if (generation !== this.generation) return false
      this.stop()
      throw error
    }
    if (generation !== this.generation) return false
    for (const track of stream.getVideoTracks())
      track.onended = () => {
        this.stop()
        this.events.ended()
      }
    this.controller = new AbortController()
    this.canvas = document.createElement('canvas')
    this.schedule()
    void this.capture()
    return true
  }

  setInterval(ms: number) {
    this.interval = ms
    if (this.stream) this.schedule()
  }

  private schedule() {
    clearInterval(this.timer)
    this.timer = setInterval(() => void this.capture(), this.interval)
  }

  private async capture() {
    if (this.busy || !this.video || !this.canvas || !this.controller) return
    const { signal } = this.controller
    this.busy = true
    try {
      if (await sendFrame(this.video, this.canvas, signal)) {
        if (!signal.aborted)
          this.events.sent(Date.now(), { width: this.canvas.width, height: this.canvas.height })
      }
    } catch {
      if (!signal.aborted) this.events.error('robotFrameFailure')
    } finally {
      if (this.controller?.signal === signal) this.busy = false
    }
  }

  stop() {
    ++this.generation
    clearInterval(this.timer)
    this.controller?.abort()
    this.controller = undefined
    this.busy = false
    this.stream?.getTracks().forEach((track) => {
      track.onended = null
      track.stop()
    })
    this.stream = undefined
    if (this.video) this.video.srcObject = null
    this.video = undefined
  }
}
