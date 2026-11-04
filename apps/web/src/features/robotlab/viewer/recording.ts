import { viewerCall } from './rpc'
import type { Frame } from './types'

/** Page offsets count wire records, including any repeated sequences. */
export async function fetchFrames(session: string, attempt: number, signal: AbortSignal) {
  const bySequence = new Map<number, Frame>()
  let from = 0
  let total = Infinity
  while (from < total) {
    signal.throwIfAborted()
    const page = await viewerCall(
      'robotlab.attempt.frames',
      { session, attempt, from, count: 400 },
      { signal },
    )
    signal.throwIfAborted()
    if (!Number.isSafeInteger(page.total) || page.total < 0 || !Array.isArray(page.frames))
      throw new Error('Invalid recording page')
    total = page.total
    if (!page.frames.length && from < total) throw new Error('Incomplete recording page')
    for (const frame of page.frames) {
      if (frame.session === session && frame.attempt === attempt && Number.isFinite(frame.t))
        bySequence.set(frame.seq, frame)
    }
    from += page.frames.length
  }
  return [...bySequence.values()].sort((a, b) => a.t - b.t || a.seq - b.seq)
}

/** Latest recorded sample at or before sim time; recordings are sampled at 20 Hz. */
export function frameAt(frames: readonly Frame[], t: number): Frame | null {
  if (!frames.length) return null
  let lo = 0
  let hi = frames.length
  while (lo < hi) {
    const middle = (lo + hi) >>> 1
    if (frames[middle]!.t <= t + 1e-9) lo = middle + 1
    else hi = middle
  }
  return frames[Math.max(0, lo - 1)]!
}
export function clampTime(t: number, duration: number) {
  return Number.isFinite(t) ? Math.max(0, Math.min(t, duration)) : 0
}
export function advanceTime(t: number, elapsedMs: number, speed: number, duration: number) {
  return clampTime(t + (Math.max(0, elapsedMs) * speed) / 1000, duration)
}
