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
/** Energy threshold and silence duration are configurable; the maximum bounds each upload. */
export class VoiceActivityDetector {
  private started?: number
  private lastSpeech = 0
  readonly threshold: number
  readonly silenceMs: number
  readonly maxMs: number
  constructor(threshold = 0.025, silenceMs = 700, maxMs = 30000) {
    this.threshold = threshold
    this.silenceMs = silenceMs
    this.maxMs = maxMs
  }
  sample(energy: number, now: number): 'start' | 'end' | undefined {
    if (energy >= this.threshold) {
      this.lastSpeech = now
      if (this.started === undefined) {
        this.started = now
        return 'start'
      }
    }
    if (
      this.started !== undefined &&
      (now - this.lastSpeech >= this.silenceMs || now - this.started >= this.maxMs)
    ) {
      this.reset()
      return 'end'
    }
  }
  reset() {
    this.started = undefined
    this.lastSpeech = 0
  }
}
export function rms(samples: Float32Array): number {
  return Math.sqrt(
    samples.reduce((sum, value) => sum + value * value, 0) / Math.max(1, samples.length),
  )
}
