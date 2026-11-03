import { rpc } from '@/lib/rpc/client'
import { sendMessage, connectConversation } from '../live'
import { useChat } from '../store'
import {
  callTransition,
  rms,
  splitSentences,
  VoiceActivityDetector,
  type CallAction,
  type CallState,
  type VadOptions,
} from './segmentation'
import { BrowserRecognizer, browserVoiceSupported, speakableText, speakWithBrowser } from './speech'

/** server: daemon STT/TTS (needs credentials). browser: Web Speech API. */
export type VoiceMode = 'server' | 'browser'

export interface CallSnapshot {
  state: CallState
  energy: number
  transcript: { role: 'user' | 'assistant'; text: string }[]
  mode?: VoiceMode
  /** Words heard so far in the current turn (browser mode). */
  partial?: string
  notice?: 'callFallback'
  error?: 'callPermission' | 'callError'
}

export interface VoiceCallOptions {
  agentId?: string
  mode?: VoiceMode
  lang?: string
  vad?: VadOptions
}

/** Owns every microphone, recognizer, recorder, timer, request and playback resource for one call. */
export class VoiceCall {
  snapshot: CallSnapshot = { state: 'ready', energy: 0, transcript: [] }
  private controller = new AbortController()
  private turn?: AbortController
  private stream?: MediaStream
  private context?: AudioContext
  private recorder?: MediaRecorder
  private recognizer?: BrowserRecognizer
  private timer?: ReturnType<typeof setInterval>
  private audio?: HTMLAudioElement
  private stopAudio?: () => void
  private muted = false
  private speaker = true
  private stt: VoiceMode
  private tts: VoiceMode
  private agentId: string
  private closeConversation?: () => void
  private lang: string
  private vad: VoiceActivityDetector
  private changed: (snapshot: CallSnapshot) => void
  constructor(changed: (snapshot: CallSnapshot) => void, options: VoiceCallOptions = {}) {
    this.agentId = options.agentId ?? 'excalibur'
    this.changed = changed
    this.stt = options.mode ?? 'server'
    this.tts = this.stt
    this.lang = options.lang ?? 'es-ES'
    this.vad = new VoiceActivityDetector(options.vad)
    this.snapshot.mode = this.stt
  }
  private emit(patch: Partial<CallSnapshot>) {
    this.snapshot = { ...this.snapshot, ...patch }
    this.changed(this.snapshot)
  }
  private transition(action: CallAction) {
    this.emit({ state: callTransition(this.snapshot.state, action) })
  }
  private get live() {
    return !this.controller.signal.aborted
  }
  async start() {
    this.closeConversation = connectConversation(this.agentId)
    try {
      this.stream = await navigator.mediaDevices.getUserMedia({
        audio: { echoCancellation: true, noiseSuppression: true, autoGainControl: true },
      })
      if (!this.live) {
        this.stream.getTracks().forEach((track) => track.stop())
        return
      }
      this.context = new AudioContext()
      await this.context.resume()
      if (!this.live) return
      const source = this.context.createMediaStreamSource(this.stream)
      const analyser = this.context.createAnalyser()
      analyser.fftSize = 1024
      source.connect(analyser)
      const samples = new Float32Array(analyser.fftSize)
      this.transition('start')
      if (this.stt === 'browser') this.listen()
      this.timer = setInterval(() => {
        analyser.getFloatTimeDomainData(samples)
        const energy = this.muted ? 0 : rms(samples)
        this.emit({ energy })
        if (this.muted) return
        const boundary = this.vad.sample(
          energy,
          performance.now(),
          this.snapshot.state === 'speaking',
        )
        if (boundary === 'start') this.speechStarted()
        if (boundary === 'end') this.speechEnded()
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
  /** Browser mode keeps the recognizer warm while it may hear the user, so the first word is not cut. */
  private listen() {
    if (!this.live || this.muted || this.stt !== 'browser') return
    this.recognizer ??= new BrowserRecognizer(
      this.lang,
      () => this.heardWords(),
      (error) => this.fail(error),
    )
    this.recognizer.start()
  }
  /** Recognized words are real speech (not just noise): they may interrupt a pending reply. */
  private heardWords() {
    if (this.snapshot.state === 'speaking' || this.muted || !this.live) return
    if (this.snapshot.state !== 'listening') this.interrupt()
    this.emit({ partial: this.recognizer?.peek() })
    this.vad.markSpeech(performance.now())
  }
  private interrupt() {
    this.turn?.abort()
    this.turn = undefined
    this.stopAudio?.()
    this.transition('interrupt')
  }
  /** A peak above the noise floor: barge in on playback and start capturing. */
  private speechStarted() {
    if (!this.stream || !this.live) return
    if (this.stt === 'server') {
      if (this.snapshot.state !== 'listening') this.interrupt()
      this.record()
      return
    }
    // Browser mode: a peak stops playback at once; while thinking, only recognized
    // words interrupt (a cough or a keystroke must not discard the pending reply).
    if (this.snapshot.state === 'speaking') this.interrupt()
    if (!this.recognizer?.running) this.listen()
  }
  /** The user has been silent for the configured pause: close the turn and send it. */
  private speechEnded() {
    if (this.stt === 'server') {
      if (this.recorder?.state === 'recording') this.recorder.stop()
      return
    }
    const text = this.recognizer?.take() ?? ''
    this.emit({ partial: undefined })
    if (!text) return
    this.turn?.abort()
    const turn = new AbortController()
    this.turn = turn
    this.transition('segment')
    void this.converse(text, turn.signal)
  }
  private record() {
    this.turn?.abort()
    const turn = new AbortController()
    this.turn = turn
    const chunks: Blob[] = []
    try {
      const recorder = new MediaRecorder(this.stream!)
      this.recorder = recorder
      recorder.ondataavailable = ({ data }) => {
        if (data.size) chunks.push(data)
      }
      recorder.onerror = () => this.fail('callError')
      recorder.onstop = () => {
        if (this.live && !turn.signal.aborted && !this.muted) {
          void this.submit(new Blob(chunks, { type: recorder.mimeType }), turn.signal)
        }
      }
      recorder.start(250)
    } catch {
      this.fail('callError')
    }
  }
  private async submit(blob: Blob, signal: AbortSignal) {
    if (!blob.size) return
    this.transition('segment')
    let text: string
    try {
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
      ;({ text } = await rpc.call(
        'voice.transcribe',
        { audio_blob: audioBlob, mime: blob.type },
        { signal, timeoutMs: 60_000 },
      ))
      signal.throwIfAborted()
    } catch {
      if (signal.aborted || !this.live) return
      // Server transcription failed (credentials, quota, network): continue with the browser.
      if (browserVoiceSupported()) {
        this.stt = 'browser'
        this.tts = 'browser'
        this.emit({ mode: 'browser', notice: 'callFallback' })
        this.transition('played')
        this.listen()
        return
      }
      this.fail('callError')
      return
    }
    if (!text.trim()) {
      this.transition('played')
      return
    }
    await this.converse(text, signal)
  }
  /** Send the user's words to the chat, wait for the reply and speak it. */
  private async converse(text: string, signal: AbortSignal) {
    try {
      this.emit({ transcript: [...this.snapshot.transcript, { role: 'user', text }] })
      // Subscribe before sending: an assistant event can arrive before the send receipt.
      const before = new Set(
        (useChat.getState().live[this.agentId] ?? []).map((message) => message.id),
      )
      const reply = this.waitForReply(before, signal)
      // Attach the rejection handler immediately, including on a failed send.
      void reply.catch(() => {})
      this.transition('sent')
      const sent = await sendMessage(this.agentId, text, [], undefined, signal)
      signal.throwIfAborted()
      if (!sent) throw new Error('Message delivery failed')
      const answer = await reply
      this.emit({ transcript: [...this.snapshot.transcript, { role: 'assistant', text: answer }] })
      this.transition('reply')
      // The browser recognizer would transcribe our own voice; pause it while speaking.
      this.recognizer?.stop()
      await this.speak(speakableText(answer), signal)
      signal.throwIfAborted()
      this.transition('played')
      this.recognizer?.clear()
      this.listen()
    } catch {
      if (!signal.aborted && this.live) this.fail('callError')
    }
  }
  private async speak(text: string, signal: AbortSignal) {
    for (const sentence of splitSentences(text)) {
      signal.throwIfAborted()
      if (this.tts === 'server') {
        try {
          await this.speakServer(sentence, signal)
          continue
        } catch (error) {
          if (signal.aborted || !('speechSynthesis' in window)) throw error
          // Synthesis credentials failed: finish this answer with the browser voice.
          this.tts = 'browser'
        }
      }
      const speech = speakWithBrowser(sentence, this.lang, signal, () => !this.speaker)
      this.stopAudio = speech.stop
      try {
        await speech.done
      } finally {
        if (this.stopAudio === speech.stop) this.stopAudio = undefined
      }
    }
  }
  private async speakServer(sentence: string, signal: AbortSignal) {
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
        const reply = state.live[this.agentId]?.find(
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
      this.recognizer?.stop()
      this.recognizer?.clear()
      this.emit({ partial: undefined })
      if (this.recorder?.state === 'recording') {
        this.turn?.abort()
        this.recorder.stop()
      }
    } else if (this.snapshot.state !== 'speaking') {
      this.listen()
    }
  }
  setSpeaker(value: boolean) {
    this.speaker = value
    if (this.audio) this.audio.muted = !value
    if (!value && this.tts === 'browser') window.speechSynthesis?.cancel()
  }
  private fail(error: 'callError' | 'callPermission') {
    this.end()
    this.emit({ error })
  }
  end() {
    this.closeConversation?.()
    this.closeConversation = undefined
    this.controller.abort()
    this.turn?.abort()
    this.stopAudio?.()
    this.recognizer?.stop()
    clearInterval(this.timer)
    if (this.recorder?.state === 'recording') this.recorder.stop()
    this.stream?.getTracks().forEach((track) => track.stop())
    const context = this.context
    this.context = undefined
    if (context && context.state !== 'closed') void context.close().catch(() => {})
    this.transition('end')
  }
}
