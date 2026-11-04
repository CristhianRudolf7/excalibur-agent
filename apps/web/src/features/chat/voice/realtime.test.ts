import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  captureWorklet,
  RealtimeRecognizer,
  speechChunks,
  StreamSpeaker,
  voiceSocketURL,
} from './realtime'

class Socket {
  static OPEN = 1
  static instances: Socket[] = []
  readyState = 0
  binaryType = ''
  sent: (string | ArrayBuffer)[] = []
  onopen?: () => void
  onclose?: () => void
  onerror?: () => void
  onmessage?: (event: { data: unknown }) => void
  url: URL
  constructor(url: URL) {
    this.url = url
    Socket.instances.push(this)
  }
  send(data: string | ArrayBuffer) {
    this.sent.push(data)
  }
  open() {
    this.readyState = 1
    this.onopen?.()
  }
  event(event: unknown) {
    this.onmessage?.({ data: JSON.stringify(event) })
  }
  close() {
    this.readyState = 3
    this.onclose?.()
  }
}
class Node {
  connect = vi.fn(() => this)
  disconnect = vi.fn()
  gain = { value: 1 }
  port: { onmessage?: ((e: { data: ArrayBuffer }) => void) | null } = {}
}
class Source extends Node {
  buffer?: { duration: number }
  onended?: (() => void) | null
  start = vi.fn()
  stop = vi.fn()
}
class Context {
  static instances: Context[] = []
  currentTime = 1
  state = 'running'
  destination = new Node()
  audioWorklet = { addModule: vi.fn(async () => {}) }
  sources: Source[] = []
  gain = new Node()
  options: unknown
  constructor(options: unknown) {
    this.options = options
    Context.instances.push(this)
  }
  resume = vi.fn(async () => {})
  close = vi.fn(async () => {
    this.state = 'closed'
  })
  createMediaStreamSource = vi.fn(() => new Node())
  createGain = () => this.gain
  createBuffer = vi.fn((_channels: number, size: number, rate: number) => ({
    duration: size / rate,
    getChannelData: () => new Float32Array(size),
  }))
  createBufferSource() {
    const source = new Source()
    this.sources.push(source)
    return source
  }
}
const worklets: Node[] = []
class Worklet extends Node {
  constructor() {
    super()
    worklets.push(this)
  }
}
const flush = async () => {
  for (let i = 0; i < 12; i++) await Promise.resolve()
}
const lastSocket = () => Socket.instances.at(-1)!
function pcm(id: number, samples = [32767, -32768]) {
  const frame = new ArrayBuffer(4 + samples.length * 2)
  const view = new DataView(frame)
  view.setUint32(0, id)
  samples.forEach((sample, index) => view.setInt16(4 + index * 2, sample, true))
  return frame
}
beforeEach(() => {
  vi.useFakeTimers()
  Socket.instances = []
  Context.instances = []
  worklets.length = 0
  vi.stubGlobal('WebSocket', Socket)
  vi.stubGlobal('AudioContext', Context)
  vi.stubGlobal('AudioWorkletNode', Worklet)
  vi.stubGlobal(
    'URL',
    class extends URL {
      static createObjectURL = vi.fn(() => 'blob:capture')
      static revokeObjectURL = vi.fn()
    },
  )
})
afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

async function recognizer(isLive?: () => boolean) {
  const activity = vi.fn()
  const failure = vi.fn()
  const instance = new RealtimeRecognizer({} as MediaStream, 'en-US', activity, failure, isLive)
  const connecting = instance.connect()
  await flush()
  const socket = lastSocket()
  socket.open()
  socket.event({ type: 'ready' })
  await connecting
  return { instance, socket, activity, failure }
}

describe('streaming recognition', () => {
  it('waits for ready, accumulates deltas per item, commits and consumes a final', async () => {
    const { instance, socket, activity } = await recognizer()
    expect(instance.running).toBe(true)
    socket.event({ type: 'delta', item: 'one', text: 'Hello' })
    socket.event({ type: 'delta', item: 'one', text: ' world' })
    expect(instance.peek()).toBe('Hello world')
    expect(activity).toHaveBeenCalledTimes(2)
    const finished = instance.finish()
    expect(socket.sent.at(-1)).toBe('{"type":"commit"}')
    socket.event({ type: 'final', item: 'one', text: 'Hello world.' })
    expect(await finished).toBe('Hello world.')
    expect(instance.peek()).toBe('')
    socket.event({ type: 'final', item: 'one', text: 'stale' })
    expect(instance.peek()).toBe('')
    instance.close()
  })
  it('returns partial text after 1.5 seconds and ignores finals after clear', async () => {
    const { instance, socket } = await recognizer()
    socket.event({ type: 'delta', item: 'a', text: 'Partial' })
    const finished = instance.finish()
    await vi.advanceTimersByTimeAsync(1500)
    expect(await finished).toBe('Partial')
    socket.event({ type: 'delta', item: 'b', text: 'Discard' })
    instance.clear()
    socket.event({ type: 'final', item: 'b', text: 'Discarded final' })
    socket.event({ type: 'final', item: 'unknown', text: 'Old unknown final' })
    expect(instance.take()).toBe('')
    instance.close()
  })
  it('sends the last 400 ms before live gated frames, in order; mute drops pre-roll', async () => {
    const { instance, socket } = await recognizer()
    const frames = Array.from({ length: 6 }, () => new ArrayBuffer(4800))
    frames.slice(0, 5).forEach((data) => worklets[0]!.port.onmessage?.({ data }))
    expect(socket.sent).toEqual([])
    instance.open()
    worklets[0]!.port.onmessage?.({ data: frames[5]! })
    expect(socket.sent).toEqual(frames.slice(1))
    instance.closeGate()
    worklets[0]!.port.onmessage?.({ data: frames[0]! })
    instance.stop()
    instance.start()
    instance.open()
    expect(socket.sent).toHaveLength(5)
    instance.close()
    expect(Context.instances[0]!.close).toHaveBeenCalled()
    expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:capture')
  })
  it.each(['error', 'close'])('rejects startup on %s without a mid-call failure', async (kind) => {
    const failure = vi.fn()
    const instance = new RealtimeRecognizer({} as MediaStream, 'es', vi.fn(), failure)
    const connected = instance.connect()
    const rejected = expect(connected).rejects.toThrow()
    await flush()
    if (kind === 'close') lastSocket().close()
    else lastSocket().event({ type: 'error', code: 'unavailable' })
    await rejected
    expect(failure).not.toHaveBeenCalled()
  })
  it('times out readiness and rejects without AudioWorklet', async () => {
    const instance = new RealtimeRecognizer({} as MediaStream, 'es', vi.fn(), vi.fn())
    const rejected = expect(instance.connect()).rejects.toThrow('timeout')
    await flush()
    await vi.advanceTimersByTimeAsync(5000)
    await rejected
    class Unsupported extends Context {
      audioWorklet = undefined as never
    }
    vi.stubGlobal('AudioContext', Unsupported)
    await expect(
      new RealtimeRecognizer({} as MediaStream, 'es', vi.fn(), vi.fn()).connect(),
    ).rejects.toThrow('unavailable')
  })
  it('bounds worklet initialization by the same five second connection deadline', async () => {
    class SlowContext extends Context {
      audioWorklet = { addModule: vi.fn(() => new Promise<void>(() => {})) }
    }
    vi.stubGlobal('AudioContext', SlowContext)
    const instance = new RealtimeRecognizer({} as MediaStream, 'en', vi.fn(), vi.fn())
    const rejected = expect(instance.connect()).rejects.toThrow('timeout')
    await flush()
    await vi.advanceTimersByTimeAsync(5000)
    await rejected
    expect(Context.instances[0]!.close).toHaveBeenCalled()
  })
  it('never retries unavailable and reports it only once', async () => {
    const { instance, socket, failure } = await recognizer()
    socket.event({ type: 'error', code: 'unavailable' })
    socket.close()
    expect(failure).toHaveBeenCalledTimes(1)
    expect(Socket.instances).toHaveLength(1)
    instance.close()
    expect(failure).toHaveBeenCalledTimes(1)
  })
  it.each(['failed', 'close'])(
    'recovers from %s with the existing capture, gate and words',
    async (kind) => {
      const { instance, socket, failure } = await recognizer()
      instance.open()
      socket.event({ type: 'delta', item: 'one', text: 'Already heard' })
      socket.event({ type: 'final', item: 'one', text: 'Already committed.' })
      socket.event({ type: 'delta', item: 'two', text: 'Partial' })
      if (kind === 'close') socket.close()
      else socket.event({ type: 'error', code: 'failed' })
      const retry = lastSocket()
      expect(retry).not.toBe(socket)
      expect(failure).not.toHaveBeenCalled()
      expect(instance.peek()).toBe('Already committed. Partial')
      retry.open()
      await vi.advanceTimersByTimeAsync(2999)
      retry.event({ type: 'ready' })
      await flush()
      retry.event({ type: 'delta', item: 'two', text: 'continued' })
      socket.event({ type: 'delta', item: 'stale', text: 'stale' })
      expect(instance.peek()).toBe('Already committed. Partial continued')
      const frame = new ArrayBuffer(4800)
      worklets[0]!.port.onmessage?.({ data: frame })
      expect(retry.sent).toEqual([frame])
      expect(Context.instances).toHaveLength(1)
      expect(worklets).toHaveLength(1)
      expect(Context.instances[0]!.close).not.toHaveBeenCalled()
      expect(failure).not.toHaveBeenCalled()
      instance.close()
    },
  )
  it('resolves an in-flight finish with its partial while reconnecting', async () => {
    const { instance, socket, failure } = await recognizer()
    socket.event({ type: 'delta', item: 'one', text: 'Committed partial' })
    const finished = instance.finish()
    socket.close()
    expect(await finished).toBe('Committed partial')
    expect(instance.peek()).toBe('')
    lastSocket().open()
    lastSocket().event({ type: 'ready' })
    await flush()
    expect(failure).not.toHaveBeenCalled()
    instance.close()
  })
  it.each(['error', 'close', 'timeout'])(
    'falls back once if the retry fails with %s, within three seconds',
    async (kind) => {
      const { instance, socket, failure } = await recognizer()
      socket.close()
      const retry = lastSocket()
      if (kind === 'error') retry.onerror?.()
      else if (kind === 'close') retry.close()
      else await vi.advanceTimersByTimeAsync(3000)
      await flush()
      expect(failure).toHaveBeenCalledExactlyOnceWith('closed')
      expect(Socket.instances).toHaveLength(2)
      expect(retry.readyState).toBe(3)
      instance.close()
    },
  )
  it('falls back on a second drop within thirty seconds of recovery', async () => {
    const { instance, socket, failure } = await recognizer()
    socket.close()
    const retry = lastSocket()
    retry.open()
    retry.event({ type: 'ready' })
    await vi.advanceTimersByTimeAsync(29_999)
    retry.close()
    await flush()
    expect(failure).toHaveBeenCalledExactlyOnceWith('closed')
    expect(Socket.instances).toHaveLength(2)
    instance.close()
  })
  it('allows another recovery after thirty seconds of a healthy connection', async () => {
    const { instance, socket, failure } = await recognizer()
    socket.close()
    lastSocket().open()
    lastSocket().event({ type: 'ready' })
    await vi.advanceTimersByTimeAsync(30_000)
    lastSocket().close()
    expect(Socket.instances).toHaveLength(3)
    lastSocket().open()
    lastSocket().event({ type: 'ready' })
    await flush()
    expect(failure).not.toHaveBeenCalled()
    instance.close()
  })
  it('cancels a retry on hangup without fallback or a later socket', async () => {
    const { instance, socket, failure } = await recognizer()
    socket.close()
    const retry = lastSocket()
    instance.close()
    retry.event({ type: 'ready' })
    await vi.advanceTimersByTimeAsync(5000)
    expect(retry.readyState).toBe(3)
    expect(instance.running).toBe(false)
    expect(failure).not.toHaveBeenCalled()
    expect(Socket.instances).toHaveLength(2)
    expect(vi.getTimerCount()).toBe(0)
  })
  it('does not reconnect when the owner is no longer live, or unmute on recovery', async () => {
    let live = true
    const { instance, socket, failure } = await recognizer(() => live)
    instance.stop()
    socket.close()
    lastSocket().open()
    lastSocket().event({ type: 'ready' })
    await flush()
    expect(instance.running).toBe(false)
    live = false
    lastSocket().close()
    expect(Socket.instances).toHaveLength(2)
    expect(failure).not.toHaveBeenCalled()
    instance.close()
  })
  it('encodes clamped PCM little endian in the worklet', () => {
    let Processor: new () => { process: (inputs: number[][][]) => void }
    const frames: ArrayBuffer[] = []
    class Base {
      port = { postMessage: (data: ArrayBuffer) => frames.push(data) }
    }
    new Function('AudioWorkletProcessor', 'registerProcessor', captureWorklet)(
      Base,
      (_name: string, ctor: typeof Processor) => {
        Processor = ctor
      },
    )
    const processor = new Processor!()
    processor.process([[Array.from({ length: 2400 }, (_, i) => (i % 2 ? -2 : 2))]])
    const view = new DataView(frames[0]!)
    expect(view.getInt16(0, true)).toBe(32767)
    expect(view.getInt16(2, true)).toBe(-32768)
  })
})

describe('streaming synthesis', () => {
  async function speaking(text = 'One sentence. Another sentence.') {
    const instance = new StreamSpeaker()
    const controller = new AbortController()
    const done = instance.speak(text, controller.signal)
    await flush()
    const socket = lastSocket()
    socket.open()
    await flush()
    return { instance, controller, done, socket, context: Context.instances[0]! }
  }
  it('makes one request for a short answer, decodes framed PCM, discards stale ids and waits for playback', async () => {
    const { instance, done, socket, context } = await speaking()
    expect(socket.sent.map(String).map((s) => JSON.parse(s))).toEqual([
      { type: 'speak', id: 1, text: 'One sentence. Another sentence.' },
    ])
    socket.onmessage?.({ data: pcm(99) })
    expect(context.sources).toHaveLength(0)
    socket.onmessage?.({ data: pcm(1) })
    socket.onmessage?.({ data: pcm(1) })
    expect(context.sources[0]!.start).toHaveBeenCalledWith(1.06)
    expect(context.sources[1]!.start).toHaveBeenCalledWith(1.06 + 2 / 24000)
    const complete = vi.fn()
    void done.then(complete)
    socket.event({ type: 'done', id: 1 })
    await flush()
    expect(complete).not.toHaveBeenCalled()
    context.sources.forEach((source) => source.onended?.())
    await done
    expect(complete).toHaveBeenCalledOnce()
    instance.close()
  })
  it('stops scheduled sources and sends cancel immediately on abort', async () => {
    const { instance, done, socket, context, controller } = await speaking()
    const rejected = expect(done).rejects.toThrow('canceled')
    socket.onmessage?.({ data: pcm(1) })
    controller.abort()
    await rejected
    expect(context.sources[0]!.stop).toHaveBeenCalledOnce()
    expect(JSON.parse(String(socket.sent.at(-1)))).toEqual({ type: 'cancel', id: 1 })
    socket.onmessage?.({ data: pcm(1) })
    expect(context.sources).toHaveLength(1)
    instance.close()
  })
  it('restarts a late schedule with jitter margin and mutes through gain', async () => {
    const { instance, done, socket, context } = await speaking()
    context.currentTime = 4
    instance.setMuted(true)
    socket.onmessage?.({ data: pcm(1) })
    expect(context.sources[0]!.start).toHaveBeenCalledWith(4.06)
    expect(context.gain.gain.value).toBe(0)
    socket.event({ type: 'done', id: 1 })
    context.sources[0]!.onended?.()
    await done
    instance.close()
  })
  it('rejects with the server code and reconnects on the next speak', async () => {
    const { instance, done, socket } = await speaking()
    const rejected = expect(done).rejects.toThrow('unavailable')
    socket.event({ type: 'error', id: 1, code: 'unavailable' })
    await rejected
    socket.close()
    const next = instance.speak('Next.', new AbortController().signal)
    await flush()
    lastSocket().open()
    await flush()
    lastSocket().event({ type: 'done', id: 2 })
    await next
    expect(Socket.instances).toHaveLength(2)
    instance.close()
  })
  it('splits long text into sequential bounded requests', async () => {
    const text = 'An ordinary sentence. '.repeat(300)
    const { instance, done, socket } = await speaking(text)
    const chunks = speechChunks(text)
    for (let i = 0; i < chunks.length; i++) {
      expect(socket.sent).toHaveLength(i + 1)
      const request = JSON.parse(String(socket.sent[i]))
      expect(request.text.length).toBeLessThanOrEqual(4000)
      socket.event({ type: 'done', id: request.id })
      await flush()
    }
    await done
    instance.close()
    expect(speechChunks('x'.repeat(9000)).map((text) => text.length)).toEqual([4000, 4000, 1000])
  })
  it('allows a long chunk to play for more than two minutes', async () => {
    const { instance, done, socket } = await speaking('A long sentence. '.repeat(200))
    const settled = vi.fn()
    void done.then(settled, settled)
    await vi.advanceTimersByTimeAsync(121000)
    expect(settled).not.toHaveBeenCalled()
    socket.event({ type: 'done', id: 1 })
    await done
    instance.close()
  })
  it('aborts while connecting and releases contexts on hangup', async () => {
    const instance = new StreamSpeaker()
    const controller = new AbortController()
    const rejected = expect(instance.speak('Hello', controller.signal)).rejects.toThrow('Canceled')
    await flush()
    controller.abort()
    await rejected
    instance.close()
    expect(Context.instances[0]!.close).toHaveBeenCalledOnce()
    expect(lastSocket().readyState).toBe(3)
  })
  it('uses same-origin websocket scheme', () => {
    expect(voiceSocketURL('/ws/voice/speak').protocol).toBe('ws:')
    expect(voiceSocketURL('/ws/voice/speak').pathname).toBe('/ws/voice/speak')
  })
})
