import { splitSentences } from './segmentation'

export function voiceSocketURL(path: string): URL {
  const url = new URL(path, window.location.href)
  url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:'
  return url
}

// The worklet keeps capture off the UI thread and transfers 100 ms PCM16LE frames.
export const captureWorklet = `
class VoiceCapture extends AudioWorkletProcessor {
  constructor() { super(); this.frame = new ArrayBuffer(4800); this.view = new DataView(this.frame); this.used = 0; }
  process(inputs) {
    const samples = inputs[0]?.[0];
    if (samples) for (const value of samples) {
      const clamped = Math.max(-1, Math.min(1, value));
      this.view.setInt16(this.used * 2, clamped < 0 ? clamped * 32768 : clamped * 32767, true);
      if (++this.used === 2400) {
        this.port.postMessage(this.frame, [this.frame]);
        this.frame = new ArrayBuffer(4800); this.view = new DataView(this.frame); this.used = 0;
      }
    }
    return true;
  }
}
registerProcessor('voice-capture', VoiceCapture);
`

/** Streaming hearing, with a local gate and bounded pre-roll. No audio leaves a closed gate. */
export class RealtimeRecognizer {
  private socket?: WebSocket
  private context?: AudioContext
  private source?: MediaStreamAudioSourceNode
  private capture?: AudioWorkletNode
  private sink?: GainNode
  private wanted = false
  private gated = false
  private ready = false
  private disposed = false
  private ring: ArrayBuffer[] = []
  private ringBytes = 0
  private items = new Map<string, string>()
  private ignored = new Set<string>()
  private pending?: { resolve: (text: string) => void; timer: ReturnType<typeof setTimeout> }
  private cancelConnect?: () => void
  private recoveredAt?: number
  private session = 0
  private isLive: () => boolean
  private stream: MediaStream
  private lang: string
  private onActivity: () => void
  private onFailure: (code: string) => void
  constructor(
    stream: MediaStream,
    lang: string,
    onActivity: () => void,
    onFailure: (code: string) => void,
    isLive: () => boolean = () => true,
  ) {
    this.stream = stream
    this.lang = lang
    this.onActivity = onActivity
    this.onFailure = onFailure
    this.isLive = isLive
  }
  get running() {
    return this.wanted
  }
  async connect(): Promise<void> {
    let timeout: ReturnType<typeof setTimeout> | undefined
    try {
      await Promise.race([
        this.initialize(),
        new Promise<never>((_, reject) => {
          timeout = setTimeout(() => reject(new Error('timeout')), 5000)
        }),
      ])
    } catch (error) {
      this.close()
      throw error
    } finally {
      clearTimeout(timeout)
    }
  }
  private async initialize(): Promise<void> {
    try {
      this.context = new AudioContext({ sampleRate: 24000 })
      if (!this.context.audioWorklet) throw new Error('unavailable')
      await this.context.resume()
      if (this.disposed) throw new Error('closed')
      const url = URL.createObjectURL(new Blob([captureWorklet], { type: 'text/javascript' }))
      try {
        await this.context.audioWorklet.addModule(url)
      } finally {
        URL.revokeObjectURL(url)
      }
      if (this.disposed) throw new Error('closed')
      this.source = this.context.createMediaStreamSource(this.stream)
      this.capture = new AudioWorkletNode(this.context, 'voice-capture')
      this.sink = this.context.createGain()
      this.sink.gain.value = 0
      this.source.connect(this.capture)
      this.capture.connect(this.sink).connect(this.context.destination)
      this.capture.port.onmessage = ({ data }: MessageEvent<ArrayBuffer>) => this.frame(data)
      await this.connectSocket(5000, true)
    } catch (error) {
      this.close()
      throw error
    }
  }
  /** Replacing a socket leaves capture, the voice gate and unconsumed words intact. */
  private connectSocket(deadline: number, initial = false): Promise<void> {
    return new Promise<void>((resolve, reject) => {
      const socket = new WebSocket(
        voiceSocketURL(
          `/ws/voice/listen?language=${encodeURIComponent(this.lang.slice(0, 2).toLowerCase())}`,
        ),
      )
      const session = ++this.session
      this.socket = socket
      socket.binaryType = 'arraybuffer'
      let settled = false
      let connected = false
      let failed = false
      const finish = (code?: string) => {
        if (settled) return
        settled = true
        clearTimeout(timeout)
        this.cancelConnect = undefined
        if (code) reject(new Error(code))
        else resolve()
      }
      this.cancelConnect = () => finish('closed')
      const timeout = setTimeout(() => failure('timeout'), deadline)
      const failure = (code: string) => {
        if (failed || this.disposed || this.socket !== socket) return
        failed = true
        this.ready = false
        this.socket = undefined
        socket.close()
        if (!connected) finish(code)
        else {
          this.complete()
          void this.reconnect(code)
        }
      }
      socket.onerror = () => failure('failed')
      socket.onclose = () => failure('closed')
      socket.onmessage = ({ data }) => {
        if (this.disposed || failed || this.socket !== socket || typeof data !== 'string') return
        let event: { type: string; item?: string; text?: string; code?: string }
        try {
          event = JSON.parse(data)
        } catch {
          return
        }
        if (event.type === 'ready') {
          connected = true
          this.ready = true
          if (initial) this.start()
          else this.recoveredAt = Date.now()
          finish()
        } else if (event.type === 'error') failure(event.code ?? 'failed')
        else if (event.type === 'delta' || event.type === 'final') {
          const item = `${session}:${event.item ?? ''}`
          if (this.ignored.has(item)) return
          if (event.type === 'delta') {
            if (!this.wanted) return
            this.items.set(item, (this.items.get(item) ?? '') + (event.text ?? ''))
            this.onActivity()
          } else {
            // An unsolicited final after a drain/clear belongs to an old turn.
            if (!this.items.has(item) && !this.pending) return
            this.items.set(item, event.text ?? '')
            if (this.pending) this.complete()
          }
        }
      }
    })
  }
  private async reconnect(code: string) {
    if (this.disposed || !this.isLive()) return
    if (
      code === 'unavailable' ||
      (this.recoveredAt !== undefined && Date.now() - this.recoveredAt < 30_000)
    ) {
      this.onFailure(code)
      return
    }
    try {
      await this.connectSocket(3000)
    } catch {
      if (!this.disposed && this.isLive()) this.onFailure(code)
    }
  }

  start() {
    this.wanted = true
  }
  stop() {
    this.wanted = false
    this.closeGate()
    this.ring = []
    this.ringBytes = 0
  }
  private send(value: string | ArrayBuffer) {
    if (this.ready && this.socket?.readyState === WebSocket.OPEN) this.socket.send(value)
  }
  private frame(frame: ArrayBuffer) {
    if (!this.wanted || this.disposed) return
    if (this.gated) this.send(frame)
    else {
      this.ring.push(frame)
      this.ringBytes += frame.byteLength
      while (this.ringBytes > 24000 * 2 * 0.4) {
        this.ringBytes -= this.ring.shift()!.byteLength
      }
    }
  }
  open() {
    if (!this.wanted || this.gated) return
    this.gated = true
    for (const frame of this.ring) this.send(frame)
    this.ring = []
    this.ringBytes = 0
  }
  closeGate() {
    this.gated = false
  }
  peek() {
    return [...this.items.values()].join(' ').trim()
  }
  take() {
    const text = this.peek()
    for (const item of this.items.keys()) this.ignored.add(item)
    this.items.clear()
    return text
  }
  clear() {
    this.take()
    this.ring = []
    this.ringBytes = 0
    this.send(JSON.stringify({ type: 'clear' }))
    if (this.pending) this.complete()
  }
  finish(): Promise<string> {
    this.closeGate()
    if (this.pending) return Promise.resolve('')
    return new Promise((resolve) => {
      this.pending = { resolve, timer: setTimeout(() => this.complete(), 1500) }
      this.send(JSON.stringify({ type: 'commit' }))
    })
  }
  private complete() {
    const pending = this.pending
    this.pending = undefined
    if (!pending) return
    clearTimeout(pending.timer)
    pending.resolve(this.take())
  }
  close() {
    this.disposed = true
    this.ready = false
    this.stop()
    this.cancelConnect?.()
    this.complete()
    this.socket?.close()
    this.source?.disconnect()
    this.capture?.disconnect()
    this.sink?.disconnect()
    if (this.capture) this.capture.port.onmessage = null
    if (this.context && this.context.state !== 'closed') void this.context.close().catch(() => {})
  }
}

/** Short answers stay a single synthesis request; oversized sentences are bounded too. */
export function speechChunks(text: string): string[] {
  if (text.length <= 4096) return text.trim() ? [text] : []
  const chunks: string[] = []
  let chunk = ''
  for (const sentence of splitSentences(text)) {
    if (chunk && chunk.length + sentence.length + 1 > 4000) {
      chunks.push(chunk)
      chunk = ''
    }
    let rest = sentence
    while (rest.length > 4000) {
      const space = rest.lastIndexOf(' ', 4000)
      const end = space > 0 ? space : 4000
      chunks.push(rest.slice(0, end))
      rest = rest.slice(end).trimStart()
    }
    chunk = chunk ? `${chunk} ${rest}` : rest
  }
  if (chunk) chunks.push(chunk)
  return chunks
}

interface Playback {
  id: number
  done: boolean
  next: number
  sources: Set<AudioBufferSourceNode>
  finish: (code?: string) => void
}

/** One reusable synthesis socket and a gapless, interruptible PCM playback queue. */
export class StreamSpeaker {
  private socket?: WebSocket
  private connecting?: Promise<void>
  private cancelConnect?: () => void
  private context?: AudioContext
  private gain?: GainNode
  private muted = false
  private disposed = false
  private id = 0
  private playback?: Playback
  setMuted(value: boolean) {
    this.muted = value
    if (this.gain) this.gain.gain.value = value ? 0 : 1
  }
  private connect() {
    if (this.disposed) return Promise.reject(new Error('closed'))
    if (this.connecting) return this.connecting
    if (this.socket?.readyState === WebSocket.OPEN) return Promise.resolve()
    const connecting = new Promise<void>((resolve, reject) => {
      const socket = new WebSocket(voiceSocketURL('/ws/voice/speak'))
      this.socket = socket
      socket.binaryType = 'arraybuffer'
      let settled = false
      const finish = (code?: string) => {
        if (settled) return
        settled = true
        clearTimeout(timeout)
        this.cancelConnect = undefined
        if (code) reject(new Error(code))
        else resolve()
      }
      this.cancelConnect = () => finish('closed')
      const timeout = setTimeout(() => {
        finish('timeout')
        socket.close()
      }, 5000)
      socket.onopen = () => finish()
      socket.onclose = socket.onerror = () => {
        finish('closed')
        if (this.socket === socket) this.playback?.finish('closed')
      }
      socket.onmessage = ({ data }) => {
        if (this.socket === socket) this.receive(data)
      }
    })
    this.connecting = connecting
    void connecting
      .finally(() => {
        if (this.connecting === connecting) this.connecting = undefined
      })
      .catch(() => {})
    return connecting
  }
  private receive(data: unknown) {
    const playback = this.playback
    if (!playback) return
    if (typeof data === 'string') {
      let event: { id: number; type: string; code?: string }
      try {
        event = JSON.parse(data)
      } catch {
        return
      }
      if (event.id !== playback.id) return
      if (event.type === 'error') playback.finish(event.code ?? 'failed')
      if (event.type === 'done') {
        playback.done = true
        if (!playback.sources.size) playback.finish()
      }
      return
    }
    if (!(data instanceof ArrayBuffer) || data.byteLength < 6 || playback.done) return
    const view = new DataView(data)
    if (view.getUint32(0, false) !== playback.id) return
    const context = this.context!
    const buffer = context.createBuffer(1, Math.floor((data.byteLength - 4) / 2), 24000)
    const samples = buffer.getChannelData(0)
    for (let i = 0; i < samples.length; i++) samples[i] = view.getInt16(4 + i * 2, true) / 32768
    const source = context.createBufferSource()
    source.buffer = buffer
    source.connect(this.gain!)
    playback.sources.add(source)
    source.onended = () => {
      source.disconnect()
      playback.sources.delete(source)
      if (playback.done && !playback.sources.size) playback.finish()
    }
    if (playback.next < context.currentTime) playback.next = context.currentTime + 0.06
    source.start(playback.next)
    playback.next += buffer.duration
  }
  async speak(text: string, signal: AbortSignal) {
    signal.throwIfAborted()
    if (!this.context) {
      this.context = new AudioContext({ sampleRate: 24000 })
      this.gain = this.context.createGain()
      this.gain.gain.value = this.muted ? 0 : 1
      this.gain.connect(this.context.destination)
    }
    await this.context.resume()
    // Connecting is abortable too, so an acknowledgement cannot delay the real reply.
    await new Promise<void>((resolve, reject) => {
      const abort = () => reject(new DOMException('Canceled', 'AbortError'))
      signal.addEventListener('abort', abort, { once: true })
      void this.connect()
        .then(resolve, reject)
        .finally(() => signal.removeEventListener('abort', abort))
      if (signal.aborted) abort()
    })
    signal.throwIfAborted()
    for (const chunk of speechChunks(text)) await this.chunk(chunk, signal)
  }
  private chunk(text: string, signal: AbortSignal): Promise<void> {
    signal.throwIfAborted()
    return new Promise((resolve, reject) => {
      const id = ++this.id
      const finish = (code?: string) => {
        if (this.playback !== playback) return
        this.playback = undefined
        clearTimeout(timeout)
        signal.removeEventListener('abort', abort)
        for (const source of playback.sources) {
          source.onended = null
          source.stop()
          source.disconnect()
        }
        if (code) reject(new Error(code))
        else resolve()
      }
      const abort = () => {
        if (this.socket?.readyState === WebSocket.OPEN)
          this.socket.send(JSON.stringify({ type: 'cancel', id }))
        finish('canceled')
      }
      const playback: Playback = {
        id,
        done: false,
        next: this.context!.currentTime + 0.06,
        sources: new Set(),
        finish,
      }
      this.playback = playback
      const timeout = setTimeout(() => finish('timeout'), Math.max(120000, text.length * 120))
      signal.addEventListener('abort', abort, { once: true })
      this.socket!.send(JSON.stringify({ type: 'speak', id, text }))
      if (signal.aborted) abort()
    })
  }
  close() {
    this.disposed = true
    this.cancelConnect?.()
    this.playback?.finish('closed')
    this.socket?.close()
    this.gain?.disconnect()
    if (this.context && this.context.state !== 'closed') void this.context.close().catch(() => {})
  }
}
