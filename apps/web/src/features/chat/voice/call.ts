import { rpc } from '@/lib/rpc/client'
import { sendMessage } from '../live'
import { useChat } from '../store'
import {
  callTransition,
  rms,
  splitSentences,
  VoiceActivityDetector,
  type CallAction,
  type CallState,
} from './segmentation'

export interface CallSnapshot {
  state: CallState
  energy: number
  transcript: { role: 'user' | 'assistant'; text: string }[]
  error?: 'callPermission' | 'callError'
}
/** Owns every microphone, recorder, timer, request and playback resource for one call. */
export class VoiceCall {
  snapshot: CallSnapshot = { state: 'ready', energy: 0, transcript: [] }
  private controller = new AbortController()
  private turn?: AbortController
  private stream?: MediaStream
  private context?: AudioContext
  private recorder?: MediaRecorder
  private timer?: ReturnType<typeof setInterval>
  private audio?: HTMLAudioElement
  private stopAudio?: () => void
  private muted = false
  private speaker = true
  private vad: VoiceActivityDetector
  private changed: (snapshot: CallSnapshot) => void
  constructor(changed: (snapshot: CallSnapshot) => void, silenceMs = 700) {
    this.changed = changed
    this.vad = new VoiceActivityDetector(0.025, silenceMs)
  }
  private emit(patch: Partial<CallSnapshot>) {
    this.snapshot = { ...this.snapshot, ...patch }
    this.changed(this.snapshot)
  }
  private transition(action: CallAction) {
    this.emit({ state: callTransition(this.snapshot.state, action) })
  }
  async start() {
    try {
      this.stream = await navigator.mediaDevices.getUserMedia({
        audio: { echoCancellation: true, noiseSuppression: true, autoGainControl: true },
      })
      if (this.controller.signal.aborted) {
        this.stream.getTracks().forEach((track) => track.stop())
        return
      }
      this.context = new AudioContext()
      await this.context.resume()
      if (this.controller.signal.aborted) return
      const source = this.context.createMediaStreamSource(this.stream)
      const analyser = this.context.createAnalyser()
      analyser.fftSize = 1024
      source.connect(analyser)
      const samples = new Float32Array(analyser.fftSize)
      this.transition('start')
      this.timer = setInterval(() => {
        analyser.getFloatTimeDomainData(samples)
        const energy = this.muted ? 0 : rms(samples)
        this.emit({ energy })
        if (this.muted) return
        const boundary = this.vad.sample(energy, performance.now())
        if (boundary === 'start') this.record()
        if (boundary === 'end' && this.recorder?.state === 'recording') this.recorder.stop()
      }, 50)
    } catch (error) {
      this.fail(
        error instanceof DOMException &&
          ['NotAllowedError', 'NotFoundError', 'SecurityError'].includes(error.name)
          ? 'callPermission'
          : 'callError',
      )
    }
  }
  private record() {
    if (!this.stream || this.controller.signal.aborted) return
    this.turn?.abort()
    this.stopAudio?.()
    this.transition('interrupt')
    const turn = new AbortController()
    this.turn = turn
    const chunks: Blob[] = []
    try {
      const recorder = new MediaRecorder(this.stream)
      this.recorder = recorder
      recorder.ondataavailable = ({ data }) => {
        if (data.size) chunks.push(data)
      }
      recorder.onerror = () => this.fail('callError')
      recorder.onstop = () => {
        if (!this.controller.signal.aborted && !turn.signal.aborted && !this.muted) {
          void this.submit(new Blob(chunks, { type: recorder.mimeType }), turn.signal)
        }
      }
      recorder.start(250)
    } catch {
      this.fail('callError')
    }
  }
  private async submit(blob: Blob, signal: AbortSignal) {
    try {
      if (!blob.size) return
      this.transition('segment')
      const upload = await fetch('/blobs', {
        method: 'POST',
        body: blob,
        headers: { 'Content-Type': blob.type },
        signal: AbortSignal.any([signal, AbortSignal.timeout(30_000)]),
      })
      if (!upload.ok) throw new Error('Audio upload failed')
      const result: { ref?: string; digest?: string; audio_blob?: string; blob_digest?: string } =
        await upload.json()
      const audioBlob = result.ref ?? result.audio_blob ?? result.digest ?? result.blob_digest
      if (!audioBlob) throw new Error('Missing audio reference')
      const { text } = await rpc.call(
        'voice.transcribe',
        { audio_blob: audioBlob, mime: blob.type },
        { signal, timeoutMs: 60_000 },
      )
      signal.throwIfAborted()
      if (!text.trim()) {
        this.transition('played')
        return
      }
      this.emit({ transcript: [...this.snapshot.transcript, { role: 'user', text }] })
      // Subscribe before sending: an assistant event can arrive before the send receipt.
      const before = new Set((useChat.getState().live.excalibur ?? []).map((message) => message.id))
      const reply = this.waitForReply(before, signal)
      // Attach the rejection handler immediately, including on a failed send.
      void reply.catch(() => {})
      this.transition('sent')
      const sent = await sendMessage('excalibur', text, [], undefined, signal)
      signal.throwIfAborted()
      if (!sent) throw new Error('Message delivery failed')
      const answer = await reply
      this.emit({ transcript: [...this.snapshot.transcript, { role: 'assistant', text: answer }] })
      this.transition('reply')
      for (const sentence of splitSentences(answer)) {
        const speech = await rpc.call(
          'voice.speak',
          { text: sentence, format: 'mp3' },
          { signal, timeoutMs: 60_000 },
        )
        signal.throwIfAborted()
        const response = await fetch(`/blobs/${encodeURIComponent(speech.audio_blob)}`, { signal })
        if (!response.ok) throw new Error('Audio download failed')
        const url = URL.createObjectURL(new Blob([await response.blob()], { type: speech.mime }))
        try {
          await this.play(url, signal)
        } finally {
          URL.revokeObjectURL(url)
        }
      }
      this.transition('played')
    } catch {
      if (!signal.aborted && !this.controller.signal.aborted) this.fail('callError')
    }
  }
  private waitForReply(before: Set<string>, signal: AbortSignal): Promise<string> {
    return new Promise((resolve, reject) => {
      const cleanup = () => {
        unsubscribe()
        clearTimeout(timeout)
        signal.removeEventListener('abort', abort)
      }
      const abort = () => {
        cleanup()
        reject(new DOMException('Canceled', 'AbortError'))
      }
      const unsubscribe = useChat.subscribe((state) => {
        const reply = state.live.excalibur?.find(
          (message) =>
            !before.has(message.id) &&
            message.sender === 'agent' &&
            !message.interaction &&
            !message.id.startsWith('notice:'),
        )
        if (reply) {
          cleanup()
          resolve(reply.text)
        }
      })
      const timeout = setTimeout(() => {
        cleanup()
        reject(new Error('Reply timed out'))
      }, 120000)
      signal.addEventListener('abort', abort, { once: true })
      if (signal.aborted) abort()
    })
  }
  private play(url: string, signal: AbortSignal) {
    return new Promise<void>((resolve, reject) => {
      const audio = new Audio(url)
      this.audio = audio
      audio.muted = !this.speaker
      const cleanup = () => {
        clearTimeout(timeout)
        audio.pause()
        audio.removeAttribute('src')
        signal.removeEventListener('abort', abort)
        if (this.stopAudio === abort) {
          this.stopAudio = undefined
          this.audio = undefined
        }
      }
      const abort = () => {
        cleanup()
        reject(new DOMException('Canceled', 'AbortError'))
      }
      const timeout = setTimeout(() => {
        cleanup()
        reject(new Error('Playback timed out'))
      }, 120000)
      this.stopAudio = abort
      signal.addEventListener('abort', abort, { once: true })
      audio.onended = () => {
        cleanup()
        resolve()
      }
      audio.onerror = () => {
        cleanup()
        reject(new Error('Playback failed'))
      }
      void audio.play().catch((error: unknown) => {
        cleanup()
        reject(error)
      })
    })
  }
  mute(value: boolean) {
    this.muted = value
    this.stream?.getAudioTracks().forEach((track) => {
      track.enabled = !value
    })
    if (value) {
      this.vad.reset()
      if (this.recorder?.state === 'recording') {
        this.turn?.abort()
        this.recorder.stop()
      }
    }
  }
  setSpeaker(value: boolean) {
    this.speaker = value
    if (this.audio) this.audio.muted = !value
  }
  private fail(error: 'callError' | 'callPermission') {
    this.end()
    this.emit({ error })
  }
  end() {
    this.controller.abort()
    this.turn?.abort()
    this.stopAudio?.()
    clearInterval(this.timer)
    if (this.recorder?.state === 'recording') this.recorder.stop()
    this.stream?.getTracks().forEach((track) => track.stop())
    const context = this.context
    this.context = undefined
    if (context && context.state !== 'closed') void context.close().catch(() => {})
    this.transition('end')
  }
}
