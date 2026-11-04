import i18n from '@/lib/i18n'
import { RealtimeRecognizer, StreamSpeaker } from './realtime'
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
import {
  BrowserRecognizer,
  browserVoiceSupported,
  isEcho,
  speakableText,
  speakWithBrowser,
  stripEcho,
} from './speech'

export type VoiceMode = 'realtime' | 'server' | 'browser'

export interface CallSnapshot {
  state: CallState
  energy: number
  transcript: { role: 'user' | 'assistant'; text: string }[]
  mode?: VoiceMode
  /** Words heard so far in the current turn. */
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
  private recognizer?: BrowserRecognizer | RealtimeRecognizer
  private streamSpeaker?: StreamSpeaker
  private finishing = 0
  private isFinishing = false
  private resumed = false
  private ack?: AbortController
  private timer?: ReturnType<typeof setInterval>
  private audio?: HTMLAudioElement
  private stopAudio?: () => void
  private muted = false
  /** Text being spoken now, to recognize the assistant's own echo. */
  private echo = ''
  /** Reply that was playing when the user barged in; its echo is stripped from the turn. */
  private bargeEcho = ''
  private speaker = true
  private stt: VoiceMode
  private tts: 'stream' | 'server' | 'browser'
  private agentId: string
  private closeConversation?: () => void
  private lang: string
  private vad: VoiceActivityDetector
  private changed: (snapshot: CallSnapshot) => void
  constructor(changed: (snapshot: CallSnapshot) => void, options: VoiceCallOptions = {}) {
    this.agentId = options.agentId ?? 'excalibur'
    this.changed = changed
    this.stt = options.mode ?? 'realtime'
    this.tts = this.stt === 'realtime' ? 'stream' : this.stt
    this.lang = options.lang ?? 'es-ES'
    this.vad = new VoiceActivityDetector({
      silenceMs: this.stt === 'realtime' ? 2000 : 3000,
      ...options.vad,
    })
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
      if (this.stt === 'realtime') {
        const recognizer = new RealtimeRecognizer(
          this.stream,
          this.lang,
          () => this.heardWords(),
          () => this.fallback(),
          () => this.live,
        )
        this.recognizer = recognizer
        this.streamSpeaker = new StreamSpeaker()
        try {
          await recognizer.connect()
        } catch {
          // Nothing was said yet, so the user has nothing to repeat: switch silently.
          if (this.live) this.fallback(false)
        }
        if (!this.live) return
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
    if (this.muted || !this.live) return
    if (this.isFinishing) {
      this.emit({ partial: this.recognizer?.peek() })
      return
    }
    if (this.snapshot.state === 'speaking' || this.echo) {
      // The recognizer keeps running during playback so the user can talk over
      // the reply; the assistant's own voice coming back is ignored.
      if (isEcho(this.recognizer?.peek() ?? '', this.echo)) return
      this.bargeEcho = this.echo
      this.interrupt()
    } else if (this.snapshot.state !== 'listening') this.interrupt()
    this.emit({ partial: this.recognizer?.peek() })
    this.vad.markSpeech(performance.now())
  }
  private interrupt() {
    this.turn?.abort()
    this.turn = undefined
    this.ack?.abort()
    this.stopAudio?.()
    this.transition('interrupt')
  }
  /** A peak above the noise floor: barge in on playback and start capturing. */
  private speechStarted() {
    if (!this.stream || !this.live) return
    if (this.stt === 'realtime') {
      if (this.isFinishing) {
        // Preserve the committed words and wait for the resumed phrase's next pause.
        this.resumed = true
      }
      ;(this.recognizer as RealtimeRecognizer).open()
      return
    }
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
  private async speechEnded() {
    if (this.stt === 'server') {
      if (this.recorder?.state === 'recording') this.recorder.stop()
      return
    }
    if (this.isFinishing) return
    const recognizer = this.recognizer
    const generation = ++this.finishing
    let text: string
    if (recognizer instanceof RealtimeRecognizer) {
      this.isFinishing = true
      this.resumed = false
      text = await recognizer.finish()
      if (generation !== this.finishing) return
      this.isFinishing = false
      if (!this.live || this.muted || recognizer !== this.recognizer) return
      if (this.resumed) {
        this.deferredText += `${text} `
        this.vad.markSpeech(performance.now())
        recognizer.open()
        return
      }
    } else text = recognizer?.take() ?? ''
    text = stripEcho(this.deferredText + text, this.bargeEcho).trim()
    this.deferredText = ''
    this.bargeEcho = ''
    this.emit({ partial: undefined })
    if (!text || isEcho(text, this.echo)) return
    this.turn?.abort()
    const turn = new AbortController()
    this.turn = turn
    this.transition('segment')
    void this.converse(text, turn.signal)
  }
  private deferredText = ''
  /** Keep the call alive when streaming hearing fails, dropping the unfinished phrase. */
  private fallback(announce = true) {
    if (!this.live || this.stt !== 'realtime') return
    this.finishing++
    this.isFinishing = false
    this.deferredText = ''
    this.recognizer?.clear()
    if (this.recognizer instanceof RealtimeRecognizer) this.recognizer.close()
    this.recognizer = undefined
    this.stt = browserVoiceSupported() ? 'browser' : 'server'
    this.vad = new VoiceActivityDetector({ silenceMs: 3000 })
    if (this.stt === 'browser') this.tts = 'browser'
    else if (this.tts === 'stream') this.tts = 'server'
    this.emit({
      mode: this.stt,
      partial: undefined,
      ...(announce ? { notice: 'callFallback' as const } : {}),
    })
    this.listen()
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
    let stopAcks = () => {}
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
      stopAcks = this.acknowledge(signal)
      void reply.then(
        () => stopAcks(),
        () => stopAcks(),
      )
      const sent = await sendMessage(this.agentId, text, [], undefined, signal)
      signal.throwIfAborted()
      if (!sent) throw new Error('Message delivery failed')
      const answer = await reply
      stopAcks()
      signal.throwIfAborted()
      this.emit({ transcript: [...this.snapshot.transcript, { role: 'assistant', text: answer }] })
      this.transition('reply')
      const spoken = speakableText(answer)
      this.recognizer?.clear()
      this.echo = spoken
      try {
        await this.speak(spoken, signal)
      } finally {
        if (this.echo === spoken) this.echo = ''
      }
      signal.throwIfAborted()
      this.transition('played')
      // Drop what the recognizer picked up of our own voice.
      this.recognizer?.clear()
      this.listen()
    } catch {
      if (!signal.aborted && this.live) this.fail('callError')
    } finally {
      stopAcks()
    }
  }
  private acknowledge(signal: AbortSignal) {
    const controller = new AbortController()
    this.ack = controller
    const combined = AbortSignal.any([signal, controller.signal])
    let playing = false
    const keys = ['callAckFirst', 'callAckWorking', 'callAckMoment'] as const
    const timers = keys.map((key, index) =>
      setTimeout(
        () => {
          if (combined.aborted || !this.live || this.snapshot.state !== 'thinking' || playing)
            return
          const text = i18n.t(key, { lng: this.lang })
          this.echo = text
          playing = true
          let spoke = false
          void this.speak(text, combined, true)
            .then(() => {
              spoke = true
            })
            .catch(() => {})
            .finally(() => {
              playing = false
              if (combined.aborted || this.echo !== text) return
              // Keep the echo reference until the reply, including delayed recognition of the ack.
              if (spoke) this.recognizer?.clear()
              else this.echo = ''
            })
        },
        2500 + index * 25000,
      ),
    )
    return () => {
      timers.forEach(clearTimeout)
      controller.abort()
      if (this.ack === controller) {
        this.ack = undefined
        this.echo = ''
      }
    }
  }
  private async speak(text: string, signal: AbortSignal, acknowledgement = false) {
    if (this.tts === 'stream') {
      try {
        await this.streamSpeaker!.speak(text, signal)
        return
      } catch (error) {
        if (signal.aborted || acknowledgement) throw error
        this.tts = 'browser'
        this.emit({ notice: 'callFallback' })
      }
    }
    for (const sentence of splitSentences(text)) {
      signal.throwIfAborted()
      if (this.tts === 'server') {
        try {
          await this.speakServer(sentence, signal)
          continue
        } catch (error) {
          if (signal.aborted || acknowledgement || !('speechSynthesis' in window)) throw error
          // Synthesis credentials failed: finish this answer with the browser voice.
          this.tts = 'browser'
          this.emit({ notice: 'callFallback' })
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
      }, 13 * 60_000)
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
      this.finishing++
      this.isFinishing = false
      this.deferredText = ''
      this.vad.reset()
      this.recognizer?.stop()
      this.recognizer?.clear()
      this.emit({ partial: undefined })
      if (this.recorder?.state === 'recording') {
        this.turn?.abort()
        this.recorder.stop()
      }
    } else {
      if (this.stt === 'realtime') this.recognizer?.start()
      this.listen()
    }
  }
  setSpeaker(value: boolean) {
    this.speaker = value
    this.streamSpeaker?.setMuted(!value)
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
    this.finishing++
    this.ack?.abort()
    this.turn?.abort()
    this.stopAudio?.()
    this.recognizer?.stop()
    if (this.recognizer instanceof RealtimeRecognizer) this.recognizer.close()
    this.streamSpeaker?.close()
    clearInterval(this.timer)
    if (this.recorder?.state === 'recording') this.recorder.stop()
    this.stream?.getTracks().forEach((track) => track.stop())
    const context = this.context
    this.context = undefined
    if (context && context.state !== 'closed') void context.close().catch(() => {})
    this.transition('end')
  }
}
