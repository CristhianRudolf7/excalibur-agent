export type CallState = 'ready' | 'listening' | 'transcribing' | 'thinking' | 'speaking' | 'ended'
export type CallAction = 'start' | 'segment' | 'sent' | 'reply' | 'played' | 'interrupt' | 'end'
export function callTransition(state: CallState, action: CallAction): CallState {
  if (action === 'end') return 'ended'
  if (action === 'start' && (state === 'ready' || state === 'ended')) return 'listening'
  if (state === 'ready' || state === 'ended') return state
  if (action === 'interrupt' || action === 'played') return 'listening'
  if (action === 'segment') return 'transcribing'
  if (action === 'sent' && state === 'transcribing') return 'thinking'
  if (action === 'reply' && state === 'thinking') return 'speaking'
  return state
}
export function splitSentences(text: string): string[] {
  const sentences: string[] = []
  let start = 0
  for (const match of text.matchAll(/[.!?。！？]+[”"']?(?=\s|$)/gu)) {
    sentences.push(text.slice(start, match.index + match[0].length).trim())
    start = match.index + match[0].length
  }
  if (text.slice(start).trim()) sentences.push(text.slice(start).trim())
  return sentences.filter(Boolean)
}
export interface VadOptions {
  /** Absolute minimum RMS that can count as speech. */
  minThreshold?: number
  /** Speech must exceed the noise floor by this factor. */
  ratio?: number
  /** A peak must last this long before it opens a turn (filters clicks). */
  attackMs?: number
  /** Silence that closes a turn. */
  silenceMs?: number
  /** Upper bound for a single turn. */
  maxMs?: number
}

/**
 * Adaptive energy voice detector. It tracks the background noise floor
 * (which is roughly flat) and opens a turn on a sustained peak above it; the
 * turn closes after `silenceMs` without speech. During playback (`bargeIn`)
 * the bar is raised so the assistant's own voice does not interrupt itself.
 */
export class VoiceActivityDetector {
  private started?: number
  private aboveSince?: number
  private lastSpeech = 0
  private turnEnergy = 0
  private turnSamples = 0
  floor = 0.004
  readonly minThreshold: number
  readonly ratio: number
  readonly attackMs: number
  readonly silenceMs: number
  readonly maxMs: number
  constructor(options: VadOptions = {}) {
    this.minThreshold = options.minThreshold ?? 0.012
    this.ratio = options.ratio ?? 3
    this.attackMs = options.attackMs ?? 100
    this.silenceMs = options.silenceMs ?? 3000
    this.maxMs = options.maxMs ?? 60000
  }
  get active() {
    return this.started !== undefined
  }
  threshold(bargeIn = false): number {
    const base = Math.max(this.minThreshold, this.floor * this.ratio, this.floor + 0.008)
    return bargeIn ? base * 2 : base
  }
  private track(energy: number, fast: boolean) {
    // Fall quickly to quiet levels, rise slowly so speech does not become "noise".
    const rate = energy < this.floor ? 0.2 : fast ? 0.05 : 0.004
    this.floor = Math.min(0.2, Math.max(0.0005, this.floor + (energy - this.floor) * rate))
  }
  sample(energy: number, now: number, bargeIn = false): 'start' | 'end' | undefined {
    const threshold = this.threshold(bargeIn)
    if (this.started === undefined) {
      if (energy >= threshold) {
        this.aboveSince ??= now
        const attack = bargeIn ? Math.max(this.attackMs, 250) : this.attackMs
        if (now - this.aboveSince >= attack) {
          this.started = this.aboveSince
          this.lastSpeech = now
          this.aboveSince = undefined
          return 'start'
        }
        return
      }
      this.aboveSince = undefined
      // Playback echo raises the floor quickly so it stops looking like a peak.
      this.track(energy, bargeIn)
      return
    }
    this.turnEnergy += energy
    this.turnSamples++
    if (energy >= threshold * 0.6) this.lastSpeech = now
    else this.track(energy, false)
    if (now - this.started >= this.maxMs) {
      // A turn that never pauses is most likely new steady noise: adopt it as the floor.
      this.floor = Math.min(0.2, this.turnEnergy / Math.max(1, this.turnSamples))
      this.reset()
      return 'end'
    }
    if (now - this.lastSpeech >= this.silenceMs) {
      this.reset()
      return 'end'
    }
  }
  /** External evidence of speech (for example a recognizer result). */
  markSpeech(now: number): 'start' | undefined {
    this.lastSpeech = now
    if (this.started === undefined) {
      this.started = now
      return 'start'
    }
  }
  reset() {
    this.started = undefined
    this.aboveSince = undefined
    this.lastSpeech = 0
    this.turnEnergy = 0
    this.turnSamples = 0
  }
}
export function rms(samples: Float32Array): number {
  return Math.sqrt(
    samples.reduce((sum, value) => sum + value * value, 0) / Math.max(1, samples.length),
  )
}
