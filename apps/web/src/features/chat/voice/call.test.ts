import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { rpc } from '@/lib/rpc/client'
import { VoiceCall } from './call'
import { RealtimeRecognizer, StreamSpeaker } from './realtime'
import { sendMessage } from '../live'
import { speakWithBrowser } from './speech'

const fake = vi.hoisted(() => ({
  supported: true,
  connectError: false,
  streamError: false,
  energy: 0,
  listeners: new Set<(state: unknown) => void>(),
  state: { live: {} } as { live: Record<string, { id: string; sender: string; text: string }[]> },
  stopTrack: vi.fn(),
  closeConversation: vi.fn(),
  browserStop: vi.fn(),
  spoken: [] as string[],
  canceled: [] as string[],
}))
vi.mock('../live', () => ({
  sendMessage: vi.fn(async () => true),
  connectConversation: () => fake.closeConversation,
}))
vi.mock('../store', () => ({
  useChat: {
    getState: () => fake.state,
    subscribe: (callback: (state: unknown) => void) => {
      fake.listeners.add(callback)
      return () => fake.listeners.delete(callback)
    },
  },
}))
vi.mock('@/lib/i18n', () => ({
  default: {
    t: (key: string) =>
      ({
        callAckFirst: 'Let me check that.',
        callAckWorking: 'Still working on it.',
        callAckMoment: 'One moment, please.',
      })[key],
  },
}))
vi.mock('./realtime', () => ({
  RealtimeRecognizer: class {
    static instances: unknown[] = []
    text = ''
    running = true
    activity: () => void
    failure: (code: string) => void
    isLive: () => boolean
    constructor(
      _stream: unknown,
      _lang: string,
      activity: () => void,
      failure: (code: string) => void,
      isLive: () => boolean,
    ) {
      this.activity = activity
      this.failure = failure
      this.isLive = isLive
      ;(this.constructor as unknown as { instances: unknown[] }).instances.push(this)
    }
    connect = vi.fn(async () => {
      if (fake.connectError) throw new Error('unavailable')
    })
    start = vi.fn(() => {
      this.running = true
    })
    stop = vi.fn(() => {
      this.running = false
    })
    open = vi.fn()
    closeGate = vi.fn()
    close = vi.fn()
    peek = () => this.text
    take = () => {
      const text = this.text
      this.text = ''
      return text
    }
    clear = vi.fn(() => {
      this.text = ''
    })
    finish = vi.fn(async () => this.take())
    hear(text: string) {
      this.text = text
      this.activity()
    }
  },
  StreamSpeaker: class {
    static instances: unknown[] = []
    constructor() {
      ;(this.constructor as unknown as { instances: unknown[] }).instances.push(this)
    }
    close = vi.fn()
    setMuted = vi.fn()
    speak = vi.fn((text: string, signal: AbortSignal) => {
      fake.spoken.push(text)
      if (fake.streamError) return Promise.reject(new Error('unavailable'))
      if (/check|working|moment|reply is long/i.test(text))
        return new Promise<void>((_, reject) => {
          signal.addEventListener(
            'abort',
            () => {
              fake.canceled.push(text)
              reject(new Error('canceled'))
            },
            { once: true },
          )
        })
      return Promise.resolve()
    })
  },
}))
vi.mock('./speech', async (original) => {
  const actual = await original<typeof import('./speech')>()
  return {
    ...actual,
    browserVoiceSupported: () => fake.supported,
    BrowserRecognizer: class {
      running = false
      start() {
        this.running = true
      }
      stop() {
        this.running = false
      }
      clear() {}
      peek() {
        return ''
      }
    },
    speakWithBrowser: vi.fn(() => ({ done: Promise.resolve(), stop: fake.browserStop })),
  }
})
class Context {
  static instances: Context[] = []
  state = 'running'
  constructor() {
    Context.instances.push(this)
  }
  resume = vi.fn(async () => {})
  close = vi.fn(async () => {
    this.state = 'closed'
  })
  createMediaStreamSource = () => ({ connect: vi.fn() })
  createAnalyser = () => ({
    fftSize: 1024,
    getFloatTimeDomainData: (data: Float32Array) => data.fill(fake.energy),
  })
}
type Recognizer = {
  isLive: () => boolean
  hear: (text: string) => void
  text: string
  failure: (code: string) => void
  finish: ReturnType<typeof vi.fn>
  close: ReturnType<typeof vi.fn>
  open: ReturnType<typeof vi.fn>
  clear: ReturnType<typeof vi.fn>
  running: boolean
}
type Speaker = {
  close: ReturnType<typeof vi.fn>
  setMuted: ReturnType<typeof vi.fn>
  speak: ReturnType<typeof vi.fn>
}
type Boundaries = { speechStarted: () => void; speechEnded: () => Promise<void> }
const instances = <T>(type: unknown) => (type as { instances: T[] }).instances
const flush = async () => {
  for (let i = 0; i < 15; i++) await Promise.resolve()
}
let calls: VoiceCall[] = []
async function start(mode: 'realtime' | 'browser' | 'server' = 'realtime') {
  const call = new VoiceCall(vi.fn(), { mode, lang: 'en-US' })
  calls.push(call)
  await call.start()
  return {
    call,
    boundary: call as unknown as Boundaries,
    recognizer: instances<Recognizer>(RealtimeRecognizer).at(-1)!,
    speaker: instances<Speaker>(StreamSpeaker).at(-1)!,
  }
}
async function turn() {
  const rig = await start()
  rig.boundary.speechStarted()
  rig.recognizer.hear('Please check this')
  await rig.boundary.speechEnded()
  await flush()
  return rig
}
function reply(text = 'The answer.') {
  fake.state = { live: { excalibur: [{ id: `reply-${Math.random()}`, sender: 'agent', text }] } }
  fake.listeners.forEach((callback) => callback(fake.state))
}
beforeEach(() => {
  vi.useFakeTimers()
  vi.clearAllMocks()
  fake.supported = true
  fake.connectError = false
  fake.streamError = false
  fake.energy = 0
  fake.state = { live: {} }
  fake.listeners.clear()
  fake.spoken = []
  fake.canceled = []
  instances(RealtimeRecognizer).length = 0
  instances(StreamSpeaker).length = 0
  Context.instances = []
  vi.stubGlobal('AudioContext', Context)
  vi.stubGlobal('navigator', {
    mediaDevices: {
      getUserMedia: vi.fn(async () => ({
        getTracks: () => [{ stop: fake.stopTrack }],
        getAudioTracks: () => [{ enabled: true }],
      })),
    },
  })
})
afterEach(async () => {
  calls.forEach((call) => call.end())
  calls = []
  await flush()
  vi.restoreAllMocks()
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('realtime calls', () => {
  it('allows socket recovery only for the lifetime of the call', async () => {
    const { call, recognizer } = await start()
    expect(recognizer.isLive()).toBe(true)
    call.end()
    expect(recognizer.isLive()).toBe(false)
  })
  it('shows partials, sends one final turn and speaks the answer verbatim', async () => {
    const { call, speaker } = await turn()
    expect(sendMessage).toHaveBeenCalledWith(
      'excalibur',
      'Please check this',
      [],
      undefined,
      expect.any(AbortSignal),
    )
    expect(call.snapshot.state).toBe('thinking')
    reply('Two sentences. One request.')
    await flush()
    expect(speaker.speak).toHaveBeenCalledWith(
      'Two sentences. One request.',
      expect.any(AbortSignal),
    )
    expect(call.snapshot.state).toBe('listening')
    expect(call.snapshot.transcript.map((line) => line.text)).toEqual([
      'Please check this',
      'Two sentences. One request.',
    ])
  })
  it('uses a two second pause and keeps slow speakers alive when words arrive', async () => {
    const { call, recognizer } = await start()
    recognizer.hear('A slow')
    expect(call.snapshot.partial).toBe('A slow')
    await vi.advanceTimersByTimeAsync(1500)
    recognizer.hear('A slow sentence')
    await vi.advanceTimersByTimeAsync(1500)
    expect(sendMessage).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(550)
    expect(sendMessage).toHaveBeenCalledTimes(1)
  })
  it('ignores echo and bare peaks during playback; novel words barge in', async () => {
    const { call, recognizer, boundary } = await turn()
    reply('This reply is long.')
    await flush()
    expect(call.snapshot.state).toBe('speaking')
    boundary.speechStarted()
    expect(recognizer.open).toHaveBeenCalled()
    expect(fake.canceled).toEqual([])
    recognizer.hear('This reply is long')
    expect(call.snapshot.state).toBe('speaking')
    recognizer.hear('This reply is long wait please change the topic')
    await flush()
    expect(call.snapshot.state).toBe('listening')
    expect(fake.canceled).toEqual(['This reply is long.'])
    await boundary.speechEnded()
    expect(vi.mocked(sendMessage).mock.calls.at(-1)?.[1]).toBe('wait please change the topic')
  })
  it('retains the browser peak interruption rule', async () => {
    const { call, boundary } = await start('browser')
    call.snapshot = { ...call.snapshot, state: 'speaking' }
    boundary.speechStarted()
    expect(call.snapshot.state).toBe('listening')
  })
  it('falls back silently on connect and mid-call failures', async () => {
    fake.connectError = true
    const first = await start()
    expect(first.call.snapshot).toMatchObject({ mode: 'browser', state: 'listening' })
    expect(first.call.snapshot.notice).toBeUndefined()
    expect(first.call.snapshot.error).toBeUndefined()
    fake.connectError = false
    const { call, recognizer } = await start()
    recognizer.hear('Discard this partial')
    recognizer.failure('failed')
    expect(call.snapshot).toMatchObject({
      mode: 'browser',
      notice: 'callFallback',
      partial: undefined,
      state: 'listening',
    })
    expect(recognizer.close).toHaveBeenCalled()
    expect(sendMessage).not.toHaveBeenCalled()
  })
  it('keeps server fallback for browsers without recognition', async () => {
    fake.supported = false
    fake.connectError = true
    const { call } = await start()
    expect(call.snapshot.mode).toBe('server')
    expect(call.snapshot.error).toBeUndefined()
  })
  it('switches only synthesis on a speak failure and keeps browser TTS thereafter', async () => {
    const { call, recognizer, boundary, speaker } = await turn()
    fake.streamError = true
    reply('First answer.')
    await flush()
    expect(call.snapshot).toMatchObject({
      mode: 'realtime',
      notice: 'callFallback',
      state: 'listening',
    })
    expect(speakWithBrowser).toHaveBeenCalledWith(
      'First answer.',
      'en-US',
      expect.any(AbortSignal),
      expect.any(Function),
    )
    recognizer.hear('Another request')
    await boundary.speechEnded()
    reply('Second answer.')
    await flush()
    expect(speaker.speak).toHaveBeenCalledTimes(1)
    expect(speakWithBrowser).toHaveBeenCalledTimes(2)
  })
  it('mutes capture, discards partials, resumes and controls the speaker gain', async () => {
    const { call, recognizer, speaker } = await start()
    recognizer.hear('Discard me')
    call.mute(true)
    expect(recognizer.running).toBe(false)
    expect(recognizer.clear).toHaveBeenCalled()
    expect(call.snapshot.partial).toBeUndefined()
    await vi.advanceTimersByTimeAsync(5000)
    expect(sendMessage).not.toHaveBeenCalled()
    call.mute(false)
    expect(recognizer.running).toBe(true)
    call.setSpeaker(false)
    expect(speaker.setMuted).toHaveBeenCalledWith(true)
  })
  it('guards overlapping commits and a new voice start while finishing', async () => {
    const { boundary, recognizer } = await start()
    let finish!: (text: string) => void
    recognizer.finish.mockImplementationOnce(
      () =>
        new Promise<string>((resolve) => {
          finish = resolve
        }),
    )
    recognizer.hear('First phrase')
    const pending = boundary.speechEnded()
    await boundary.speechEnded()
    expect(recognizer.finish).toHaveBeenCalledTimes(1)
    boundary.speechStarted()
    finish('First phrase')
    await pending
    expect(sendMessage).not.toHaveBeenCalled()
    recognizer.text = 'second phrase'
    await boundary.speechEnded()
    expect(vi.mocked(sendMessage).mock.calls.at(-1)?.[1]).toBe('First phrase second phrase')
  })
  it('drops an in-flight commit on mute and hangup closes every resource', async () => {
    const { call, recognizer, speaker, boundary } = await start()
    let finish!: (text: string) => void
    recognizer.finish.mockImplementationOnce(
      () =>
        new Promise<string>((resolve) => {
          finish = resolve
        }),
    )
    const pending = boundary.speechEnded()
    call.mute(true)
    finish('Discard')
    await pending
    expect(sendMessage).not.toHaveBeenCalled()
    call.end()
    expect(call.snapshot.state).toBe('ended')
    expect(recognizer.close).toHaveBeenCalled()
    expect(speaker.close).toHaveBeenCalled()
    expect(Context.instances.every((context) => context.close.mock.calls.length > 0)).toBe(true)
    expect(fake.stopTrack).toHaveBeenCalled()
    expect(fake.closeConversation).toHaveBeenCalled()
    expect(fake.listeners.size).toBe(0)
  })
})

describe('waiting acknowledgements', () => {
  it('speaks at 2.5 seconds then every 25 seconds, at most three, without transcribing them', async () => {
    const { call } = await turn()
    await vi.advanceTimersByTimeAsync(2499)
    expect(fake.spoken).toEqual([])
    await vi.advanceTimersByTimeAsync(1)
    expect(fake.spoken).toEqual(['Let me check that.'])
    // Complete each ack to allow the next one.
    // A fresh rig with immediately completed speech exercises all three timers.
    call.end()
    const { speaker, call: next } = await turn()
    speaker.speak.mockImplementation(async (text: string) => {
      fake.spoken.push(text)
    })
    fake.spoken = []
    await vi.advanceTimersByTimeAsync(60000)
    expect(fake.spoken).toEqual([
      'Let me check that.',
      'Still working on it.',
      'One moment, please.',
    ])
    expect(next.snapshot.state).toBe('thinking')
    expect(next.snapshot.transcript).toEqual([{ role: 'user', text: 'Please check this' }])
  })
  it('cancels a playing acknowledgement as soon as the reply arrives', async () => {
    const { call } = await turn()
    await vi.advanceTimersByTimeAsync(2500)
    expect(call.snapshot.state).toBe('thinking')
    reply()
    await flush()
    expect(fake.canceled).toContain('Let me check that.')
    expect(fake.spoken.at(-1)).toBe('The answer.')
    await vi.advanceTimersByTimeAsync(60000)
    expect(fake.spoken).toEqual(['Let me check that.', 'The answer.'])
    expect(call.snapshot.transcript).toHaveLength(2)
  })
  it('ignores acknowledgement echo, stops on barge-in and silently ignores acknowledgement failure', async () => {
    const { recognizer, call } = await turn()
    await vi.advanceTimersByTimeAsync(2500)
    recognizer.hear('Let me check that')
    expect(call.snapshot.state).toBe('thinking')
    recognizer.hear('Wait please do something different')
    await flush()
    expect(fake.canceled).toContain('Let me check that.')
    expect(call.snapshot.state).toBe('listening')
    const next = await turn()
    fake.streamError = true
    await vi.advanceTimersByTimeAsync(2500)
    expect(next.call.snapshot.error).toBeUndefined()
    expect(next.call.snapshot.notice).toBeUndefined()
    expect(next.call.snapshot.state).toBe('thinking')
  })
  it('allows thirteen minutes for the reply', async () => {
    const { call } = await turn()
    await vi.advanceTimersByTimeAsync(12 * 60_000)
    expect(call.snapshot.state).toBe('thinking')
    reply()
    await flush()
    expect(call.snapshot.error).toBeUndefined()
  })
})

it.each(['browser', 'server'] as const)(
  'acknowledges in %s mode too, ignoring synthesis errors',
  async (mode) => {
    const serverSpeech = vi.spyOn(rpc, 'call').mockRejectedValue(new Error('unavailable'))
    const { call } = await start(mode)
    call.snapshot = { ...call.snapshot, state: 'transcribing' }
    const conversation = (
      call as unknown as { converse: (text: string, signal: AbortSignal) => Promise<void> }
    ).converse('A question', new AbortController().signal)
    await flush()
    await vi.advanceTimersByTimeAsync(2500)
    if (mode === 'browser')
      expect(speakWithBrowser).toHaveBeenCalledWith(
        'Let me check that.',
        'en-US',
        expect.any(AbortSignal),
        expect.any(Function),
      )
    else
      expect(serverSpeech).toHaveBeenCalledWith(
        'voice.speak',
        { text: 'Let me check that.', format: 'mp3' },
        expect.any(Object),
      )
    expect(call.snapshot.state).toBe('thinking')
    expect(call.snapshot.error).toBeUndefined()
    call.end()
    reply()
    await conversation
  },
)

it('cancels acknowledgements on a reply that arrives before the send receipt', async () => {
  let receipt!: (sent: boolean) => void
  vi.mocked(sendMessage).mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        receipt = resolve
      }),
  )
  const { call } = await turn()
  await vi.advanceTimersByTimeAsync(2500)
  reply()
  await flush()
  expect(fake.canceled).toContain('Let me check that.')
  receipt(true)
  await flush()
  expect(call.snapshot.state).toBe('listening')
  expect(fake.spoken.at(-1)).toBe('The answer.')
})

it('ignores delayed acknowledgement echo after playback finishes without interrupting the reply', async () => {
  const { call, recognizer, speaker, boundary } = await turn()
  speaker.speak.mockResolvedValue(undefined)
  await vi.advanceTimersByTimeAsync(2500)
  recognizer.hear('Let me check that')
  expect(call.snapshot.state).toBe('thinking')
  await boundary.speechEnded()
  expect(sendMessage).toHaveBeenCalledTimes(1)
  expect(call.snapshot.transcript).toHaveLength(1)
  await vi.advanceTimersByTimeAsync(25000)
  expect(speaker.speak).toHaveBeenCalledTimes(2)
  reply()
  await flush()
  expect(call.snapshot.state).toBe('listening')
})
