/**
 * Browser speech helpers: markdown → speakable text, language/voice choice,
 * Web Speech synthesis and a restartable SpeechRecognition wrapper used when
 * the daemon has no voice credentials.
 */

const EMOJI =
  /[\p{Extended_Pictographic}\u{1F1E6}-\u{1F1FF}\u{1F3FB}-\u{1F3FF}\u{FE0E}\u{FE0F}\u{200D}\u{20E3}]/gu
const BULLETS = /[•●○◦▪▫■□►▶▸▹➤➜✓✔✗✘☐☑☒★☆]/gu

function cleanLine(raw: string): string {
  let line = raw.trim()
  if (!line) return ''
  // Horizontal rules and table separator rows carry no words.
  if (/^([-*_=]\s*){3,}$/.test(line)) return ''
  if (/^\|?\s*:?-{2,}:?\s*(\|\s*:?-{2,}:?\s*)*\|?$/.test(line)) return ''
  line = line
    .replace(/^#{1,6}\s+/, '')
    .replace(/^(>\s*)+/, '')
    .replace(/^([-*+]|\d{1,3}[.)])\s+/, '')
    .replace(/^\[[ xX]\]\s+/, '')
  // Table rows: read cells as a comma separated list.
  if (line.includes('|')) {
    const cells = line
      .split('|')
      .map((cell) => cell.trim())
      .filter(Boolean)
    if (cells.length > 1) line = cells.join(', ')
  }
  return line
}

const normalize = (text: string) =>
  text
    .toLocaleLowerCase()
    .replace(/[^\p{L}\p{N}\s]/gu, '')
    .replace(/\s+/g, ' ')
    .trim()

/**
 * Text of one recognition session. Consecutive results that only extend (or
 * repeat) the previous one replace it, so cumulative engines do not echo words.
 */
export function collapseRepeats(texts: readonly string[]): string {
  const parts: string[] = []
  for (const raw of texts) {
    const text = raw?.trim()
    if (!text) continue
    const previous = parts.at(-1)
    if (previous !== undefined) {
      const a = normalize(previous)
      const b = normalize(text)
      if (b.startsWith(a)) {
        parts[parts.length - 1] = text
        continue
      }
      if (a.startsWith(b)) continue
    }
    parts.push(text)
  }
  return parts.join(' ').replace(/\s+/g, ' ').trim()
}

export function sessionTranscript(
  results: ArrayLike<{ isFinal?: boolean; 0?: { transcript?: string } }>,
): string {
  return collapseRepeats(Array.from(results, (result) => result?.[0]?.transcript ?? ''))
}

/** Plain text for a speech engine: no markdown marks, code, URLs, emoji or bullets. */
export function speakableText(markdown: string): string {
  let text = markdown.replace(/\r/g, '')
  text = text
    .replace(/```[\s\S]*?(```|$)/g, '\n') // fenced code, also unterminated
    .replace(/<\/?[a-z][^>]*>/gi, ' ')
    .replace(/!\[[^\]]*\]\([^)]*\)/g, ' ')
    .replace(/\[\[doc:[^\]]+\]\]/g, ' ')
    .replace(/\[([^\]]+)\]\((?:[^()\s]|\([^)]*\))*\)/g, '$1')
    .replace(/\[\^?\d+(?:[,\s]+\^?\d+)*\]/g, '') // citation markers [1], [^2], [1, 2]
    .replace(/<?https?:\/\/[^\s)>\]]+>?/g, ' ')
  const lines = text
    .split('\n')
    .map(cleanLine)
    .filter(Boolean)
    // Headings and list items rarely end in punctuation; add a pause.
    .map((line) => (/[.!?:;,。！？…]$/.test(line) ? line : `${line}.`))
  const spoken = lines
    .join(' ')
    .replace(/`([^`]*)`/g, '$1')
    .replace(/(\*\*|__|~~)(.+?)\1/g, '$2')
    .replace(/(^|[\s(¿¡"])[*_]([^*_\s][^*_]*?)[*_](?=[\s).,;:!?"]|$)/g, '$1$2')
    .replace(EMOJI, '')
    .replace(BULLETS, ' ')
    .replace(/\s*(→|⇒|->|=>)\s*/g, ', ')
    .replace(/\s+[-–—]\s+/g, ', ')
    .replace(/[*#`~|>\\]/g, ' ')
    .replace(/\s+([.,;:!?])/g, '$1')
    .replace(/([.,;:!?])(\s*[.,;:])+/g, '$1')
    .replace(/\s+/g, ' ')
    .trim()
  return /[\p{L}\p{N}]/u.test(spoken) ? spoken : ''
}

/** BCP-47 tag for the UI language, preferring the browser's own region. */
export function speechLang(uiLanguage?: string): string {
  const base = uiLanguage?.toLowerCase().startsWith('en') ? 'en' : 'es'
  const preferred =
    typeof navigator === 'undefined'
      ? undefined
      : (navigator.languages ?? [navigator.language]).find(
          (tag) => tag?.toLowerCase().startsWith(base) && tag.includes('-'),
        )
  return preferred ?? (base === 'en' ? 'en-US' : 'es-ES')
}

export function pickVoice(lang: string): SpeechSynthesisVoice | undefined {
  const voices = typeof speechSynthesis === 'undefined' ? [] : speechSynthesis.getVoices()
  const exact = voices.filter((voice) => voice.lang.toLowerCase() === lang.toLowerCase())
  const family = voices.filter((voice) =>
    voice.lang.toLowerCase().startsWith(lang.slice(0, 2).toLowerCase()),
  )
  const pool = exact.length ? exact : family
  return pool.find((voice) => /natural|neural|google|online/i.test(voice.name)) ?? pool[0]
}

interface RecognitionResult {
  isFinal: boolean
  0: { transcript: string }
}
interface RecognitionEvent {
  resultIndex: number
  results: ArrayLike<RecognitionResult>
}
interface Recognition {
  lang: string
  continuous: boolean
  interimResults: boolean
  onresult: ((event: RecognitionEvent) => void) | null
  onerror: ((event: { error: string }) => void) | null
  onend: (() => void) | null
  start(): void
  stop(): void
  abort(): void
}
type RecognitionCtor = new () => Recognition

function recognitionCtor(): RecognitionCtor | undefined {
  if (typeof window === 'undefined') return undefined
  const w = window as unknown as {
    SpeechRecognition?: RecognitionCtor
    webkitSpeechRecognition?: RecognitionCtor
  }
  return w.SpeechRecognition ?? w.webkitSpeechRecognition
}

/** Speech-to-text only (dictation does not need speech synthesis). */
export function recognitionSupported(): boolean {
  return !!recognitionCtor()
}

export function browserVoiceSupported(): boolean {
  return (
    !!recognitionCtor() &&
    typeof window !== 'undefined' &&
    'speechSynthesis' in window &&
    typeof SpeechSynthesisUtterance !== 'undefined'
  )
}

/**
 * Continuous browser transcription. It restarts itself while wanted (engines
 * end sessions after silence) and accumulates final plus interim text until
 * `take()` drains it.
 */
export class BrowserRecognizer {
  private recognition?: Recognition
  private wanted = false
  private finals: string[] = []
  private interim = ''
  private restart?: ReturnType<typeof setTimeout>
  private lang: string
  private onActivity: () => void
  private onFatal: (error: 'callPermission' | 'callError') => void
  constructor(
    lang: string,
    onActivity: () => void,
    onFatal: (error: 'callPermission' | 'callError') => void,
  ) {
    this.lang = lang
    this.onActivity = onActivity
    this.onFatal = onFatal
  }
  get running() {
    return this.wanted
  }
  private create(): Recognition | undefined {
    const Ctor = recognitionCtor()
    if (!Ctor) return undefined
    const recognition = new Ctor()
    recognition.lang = this.lang
    recognition.continuous = true
    recognition.interimResults = true
    recognition.onresult = (event) => {
      if (recognition !== this.recognition) return
      // Rebuild the whole session from the result list instead of accumulating
      // deltas: some engines (Chrome on Android) resend growing prefixes as new
      // "final" results, which would otherwise repeat every word.
      this.interim = sessionTranscript(event.results)
      this.onActivity()
    }
    recognition.onerror = (event) => {
      if (recognition !== this.recognition) return
      if (event.error === 'not-allowed' || event.error === 'service-not-allowed') {
        this.wanted = false
        this.onFatal('callPermission')
      } else if (event.error === 'network' || event.error === 'language-not-supported') {
        this.wanted = false
        this.onFatal('callError')
      }
      // 'no-speech', 'aborted' and 'audio-capture' blips: onend restarts.
    }
    recognition.onend = () => {
      if (recognition !== this.recognition) return
      // Commit the finished session; a restart begins a fresh result list.
      if (this.interim) this.finals.push(this.interim)
      this.interim = ''
      if (this.wanted) this.restart = setTimeout(() => this.spawn(), 120)
    }
    return recognition
  }
  private spawn() {
    if (!this.wanted) return
    const recognition = this.create()
    this.recognition = recognition
    try {
      recognition?.start()
    } catch {
      this.restart = setTimeout(() => this.spawn(), 300)
    }
  }
  start() {
    if (this.wanted) return
    this.wanted = true
    this.spawn()
  }
  stop() {
    this.wanted = false
    clearTimeout(this.restart)
    const recognition = this.recognition
    this.recognition = undefined
    try {
      recognition?.abort()
    } catch {
      /* already stopped */
    }
  }
  /** Drain everything heard so far and start a fresh session so nothing leaks into the next turn. */
  take(): string {
    const text = collapseRepeats([...this.finals, this.interim])
    this.finals = []
    this.interim = ''
    if (this.wanted) {
      this.stop()
      this.start()
    }
    return text
  }
  /** Words heard so far, without draining them. */
  peek(): string {
    return collapseRepeats([...this.finals, this.interim])
  }
  clear() {
    this.finals = []
    this.interim = ''
  }
}

/** Speak one chunk with the browser engine; resolves when done, rejects on abort. */
export function speakWithBrowser(
  text: string,
  lang: string,
  signal: AbortSignal,
  muted: () => boolean,
): { done: Promise<void>; stop: () => void } {
  let stop = () => {}
  const done = new Promise<void>((resolve, reject) => {
    if (!text.trim() || muted()) return resolve()
    const synth = window.speechSynthesis
    const utterance = new SpeechSynthesisUtterance(text)
    utterance.lang = lang
    const voice = pickVoice(lang)
    if (voice) utterance.voice = voice
    let settled = false
    const finish = (error?: unknown) => {
      if (settled) return
      settled = true
      clearTimeout(timeout)
      signal.removeEventListener('abort', abort)
      if (error) reject(error)
      else resolve()
    }
    const abort = () => {
      synth.cancel()
      finish(new DOMException('Canceled', 'AbortError'))
    }
    stop = abort
    // Some engines never fire onend; bound each chunk by its length.
    const timeout = setTimeout(() => finish(), Math.max(8000, text.length * 120))
    utterance.onend = () => finish()
    utterance.onerror = (event) => {
      const error = (event as unknown as { error?: string }).error
      // Cancelled by the speaker toggle (not by an abort): treat as finished.
      if (error === 'interrupted' || error === 'canceled')
        finish(signal.aborted ? new DOMException('Canceled', 'AbortError') : undefined)
      else finish(new Error(`Speech synthesis failed: ${error ?? 'unknown'}`))
    }
    signal.addEventListener('abort', abort, { once: true })
    if (signal.aborted) return abort()
    synth.speak(utterance)
  })
  return { done, stop: () => stop() }
}
