import { describe, expect, it } from 'vitest'
import { callTransition, rms, splitSentences, VoiceActivityDetector } from './segmentation'

describe('adaptive energy VAD', () => {
  const quiet = (vad: VoiceActivityDetector, from: number, to: number, level = 0.003) => {
    let boundary: string | undefined
    for (let now = from; now <= to; now += 50) boundary = vad.sample(level, now) ?? boundary
    return boundary
  }
  it('opens on a sustained peak and sends only after a 3 s pause', () => {
    const vad = new VoiceActivityDetector()
    expect(quiet(vad, 0, 2000)).toBeUndefined()
    expect(vad.sample(0.08, 2050)).toBeUndefined() // attack: a single sample is not enough
    expect(vad.sample(0.08, 2150)).toBe('start')
    expect(vad.sample(0.08, 2500)).toBeUndefined()
    // A 2.5 s thinking pause keeps the turn open.
    expect(quiet(vad, 2550, 5000)).toBeUndefined()
    expect(vad.sample(0.08, 5050)).toBeUndefined()
    expect(quiet(vad, 5100, 8000)).toBeUndefined()
    expect(vad.sample(0.003, 8050)).toBe('end')
    expect(vad.active).toBe(false)
  })
  it('ignores single clicks and adapts to a louder but steady background', () => {
    const vad = new VoiceActivityDetector()
    expect(vad.sample(0.2, 0)).toBeUndefined()
    expect(vad.sample(0.001, 50)).toBeUndefined()
    // A fan hums at 0.01 for a while: the floor follows it.
    for (let now = 100; now < 60000; now += 50) vad.sample(0.009, now)
    expect(vad.floor).toBeGreaterThan(0.006)
    expect(vad.threshold()).toBeGreaterThan(0.02)
    expect(vad.sample(0.06, 60000)).toBeUndefined()
    expect(vad.sample(0.06, 60100)).toBe('start')
  })
  it('requires a stronger, longer peak to barge in over playback', () => {
    const vad = new VoiceActivityDetector()
    quiet(vad, 0, 1000)
    expect(vad.threshold(true)).toBeGreaterThan(vad.threshold())
    expect(vad.sample(0.015, 1050, true)).toBeUndefined()
    expect(vad.sample(0.015, 1400, true)).toBeUndefined()
    expect(vad.sample(0.1, 1450, true)).toBeUndefined()
    expect(vad.sample(0.1, 1600, true)).toBeUndefined()
    expect(vad.sample(0.1, 1700, true)).toBe('start')
  })
  it('accepts recognizer evidence, bounds endless turns and supports custom timing', () => {
    const vad = new VoiceActivityDetector({ silenceMs: 400, maxMs: 1000, attackMs: 0 })
    expect(vad.markSpeech(0)).toBe('start')
    expect(vad.markSpeech(100)).toBeUndefined()
    expect(vad.sample(0, 499)).toBeUndefined()
    expect(vad.sample(0, 500)).toBe('end')
    expect(vad.sample(0.2, 600)).toBe('start')
    expect(vad.sample(0.2, 1600)).toBe('end')
    vad.reset()
    expect(vad.active).toBe(false)
  })
  it('computes RMS without treating silence or an empty buffer as speech', () => {
    expect(rms(new Float32Array())).toBe(0)
    expect(rms(new Float32Array([0, 0, 0]))).toBe(0)
    expect(rms(new Float32Array([0.5, -0.5]))).toBe(0.5)
  })
})
describe('sentence playback segmentation', () => {
  it.each([
    ['', []],
    ['One sentence.', ['One sentence.']],
    ['Hello! How are you? A final fragment', ['Hello!', 'How are you?', 'A final fragment']],
    ['Value 3.14 is valid. Next line.\nDone!', ['Value 3.14 is valid.', 'Next line.', 'Done!']],
    ['¿Listo? ¡Vamos!\n続けます。 次へ！', ['¿Listo?', '¡Vamos!', '続けます。', '次へ！']],
  ])('splits %j without losing content', (text, expected) =>
    expect(splitSentences(text)).toEqual(expected),
  )
})
describe('call lifecycle', () => {
  it('follows listen → transcribe → think → speak → listen', () => {
    let state = callTransition('ready', 'start')
    expect(state).toBe('listening')
    state = callTransition(state, 'segment')
    expect(state).toBe('transcribing')
    state = callTransition(state, 'sent')
    expect(state).toBe('thinking')
    state = callTransition(state, 'reply')
    expect(state).toBe('speaking')
    expect(callTransition(state, 'played')).toBe('listening')
  })
  it('allows barge-in and hangup at every active stage; ignores stale replies after hangup', () => {
    for (const state of ['listening', 'transcribing', 'thinking', 'speaking'] as const) {
      expect(callTransition(state, 'interrupt')).toBe('listening')
      expect(callTransition(state, 'end')).toBe('ended')
    }
    expect(callTransition('ended', 'reply')).toBe('ended')
    expect(callTransition('ended', 'segment')).toBe('ended')
    expect(callTransition('ready', 'reply')).toBe('ready')
    expect(callTransition('listening', 'reply')).toBe('listening')
  })
})
