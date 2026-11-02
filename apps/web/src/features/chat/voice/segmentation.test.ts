import { describe, expect, it } from 'vitest'
import { callTransition, rms, splitSentences, VoiceActivityDetector } from './segmentation'

describe('energy VAD', () => {
  it('segments only speech followed by 700 ms of silence and resets between utterances', () => {
    const vad = new VoiceActivityDetector()
    expect(vad.sample(0, 0)).toBeUndefined()
    expect(vad.sample(0.08, 50)).toBe('start')
    expect(vad.sample(0.08, 300)).toBeUndefined()
    expect(vad.sample(0, 999)).toBeUndefined()
    expect(vad.sample(0, 1000)).toBe('end')
    expect(vad.sample(0, 2000)).toBeUndefined()
    expect(vad.sample(0.08, 2100)).toBe('start')
  })
  it('bounds continuous speech, supports custom sensitivity/silence, and discards muted segments', () => {
    const vad = new VoiceActivityDetector(0.1, 400, 1000)
    expect(vad.sample(0.09, 0)).toBeUndefined()
    expect(vad.sample(0.2, 10)).toBe('start')
    expect(vad.sample(0.2, 1010)).toBe('end')
    expect(vad.sample(0.2, 1050)).toBe('start')
    vad.reset()
    expect(vad.sample(0, 5000)).toBeUndefined()
    expect(vad.sample(0.2, 5010)).toBe('start')
    expect(vad.sample(0, 5410)).toBe('end')
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
