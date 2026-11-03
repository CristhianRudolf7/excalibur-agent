import { test, expect } from './fixtures'
import { mockChat } from './chat-fixture'

// Chromium supplies a real fake microphone stream. Only analyser energy is
// controlled so segment boundaries remain deterministic across machines.
test.use({
  launchOptions: {
    args: [
      '--use-fake-ui-for-media-stream',
      '--use-fake-device-for-media-stream',
      '--autoplay-policy=no-user-gesture-required',
    ],
  },
  permissions: ['microphone'],
})
function wav(seconds = 0.2) {
  const length = Math.floor(16000 * seconds)
  const data = Buffer.alloc(44 + length * 2)
  data.write('RIFF', 0)
  data.writeUInt32LE(data.length - 8, 4)
  data.write('WAVEfmt ', 8)
  data.writeUInt32LE(16, 16)
  data.writeUInt16LE(1, 20)
  data.writeUInt16LE(1, 22)
  data.writeUInt32LE(16000, 24)
  data.writeUInt32LE(32000, 28)
  data.writeUInt16LE(2, 32)
  data.writeUInt16LE(16, 34)
  data.write('data', 36)
  data.writeUInt32LE(length * 2, 40)
  return data
}
async function controlEnergy(page: import('@playwright/test').Page) {
  await page.addInitScript(() => {
    const state = window as unknown as { micEnergy: number; pauses: number; tracksStopped: number }
    state.micEnergy = 0
    state.pauses = 0
    state.tracksStopped = 0
    const create = AudioContext.prototype.createAnalyser
    AudioContext.prototype.createAnalyser = function () {
      const analyser = create.call(this)
      analyser.getFloatTimeDomainData = (samples: Float32Array<ArrayBuffer>) =>
        samples.fill(state.micEnergy)
      return analyser
    }
    const pause = HTMLMediaElement.prototype.pause
    HTMLMediaElement.prototype.pause = function () {
      state.pauses++
      pause.call(this)
    }
    const stop = MediaStreamTrack.prototype.stop
    MediaStreamTrack.prototype.stop = function () {
      state.tracksStopped++
      stop.call(this)
    }
  })
}
const energy = (page: import('@playwright/test').Page, value: number) =>
  page.evaluate((level) => {
    ;(window as unknown as { micEnergy: number }).micEnergy = level
  }, value)

test('phone call segments speech, transcribes, sends, speaks sentences in order and cleans up', async ({
  page,
}) => {
  await controlEnergy(page)
  const chat = await mockChat(page)
  const spoken: string[] = []
  let uploads = 0
  await page.route('**/blobs', (route) => {
    expect(route.request().method()).toBe('POST')
    expect(route.request().postDataBuffer()!.length).toBeGreaterThan(0)
    uploads++
    return route.fulfill({ json: { ref: 'audio-input' } })
  })
  await page.route('**/blobs/audio-output', (route) =>
    route.fulfill({ contentType: 'audio/wav', body: wav() }),
  )
  await page.route('**/rpc', async (route) => {
    const request = route.request().postDataJSON()
    const result =
      request.method === 'voice.status'
        ? { available: true, voices: ['alloy'] }
        : request.method === 'voice.transcribe'
          ? { text: 'Hello from the microphone', language: 'en', duration_ms: 1000 }
          : request.method === 'voice.speak'
            ? { audio_blob: 'audio-output', mime: 'audio/wav' }
            : undefined
    if (!result) return route.fallback()
    if (request.method === 'voice.speak') spoken.push(request.params.text)
    if (request.method === 'voice.transcribe') expect(request.params.audio_blob).toBe('audio-input')
    await route.fulfill({ json: { jsonrpc: '2.0', id: request.id, result } })
  })
  await page.goto('/agents/excalibur?view=chat')
  await page.getByRole('button', { name: 'Llamar', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'Llamada con Excalibur' })
  await expect(dialog.getByText('Escuchando', { exact: true })).toBeVisible()
  await energy(page, 0.1)
  await page.waitForTimeout(350)
  await energy(page, 0)
  await expect(dialog.getByText('Pensando', { exact: true })).toBeVisible()
  await expect.poll(() => uploads).toBe(1)
  expect(chat.messages.at(-1)?.text).toBe('Hello from the microphone')
  expect(chat.messages.at(-1)?.conversation_id).toBe('cnv_web_excalibur')
  chat.emit('message.created', {
    message_id: 'assistant-voice',
    conversation_id: 'cnv_web_excalibur',
    role: 'assistant',
    text: 'Hello there. Second sentence!',
  })
  await expect.poll(() => spoken).toEqual(['Hello there.', 'Second sentence!'])
  await expect(dialog.getByText('Escuchando', { exact: true })).toBeVisible()
  await expect(dialog.getByRole('log')).toContainText('Hello from the microphone')
  await expect(dialog.getByRole('log')).toContainText('Hello there. Second sentence!')
  // Closing the sheet keeps the call alive; its controls move into the composer.
  await dialog.getByRole('button', { name: 'Cerrar' }).click()
  await expect(dialog).toHaveCount(0)
  const composer = page.locator('.composer')
  await expect(composer.getByRole('button', { name: 'Colgar' })).toBeVisible()
  await expect(composer.getByRole('button', { name: 'Enviar mensaje' })).toHaveCount(0)
  await composer.getByRole('button', { name: 'Mostrar llamada' }).click()
  await expect(dialog.getByText('Escuchando', { exact: true })).toBeVisible()
  await dialog.getByRole('button', { name: 'Silenciar micrófono' }).click()
  await expect(dialog.getByRole('button', { name: 'Silenciar micrófono' })).toHaveAttribute(
    'aria-pressed',
    'true',
  )
  await dialog.getByRole('button', { name: 'Altavoz' }).click()
  await expect(dialog.getByRole('button', { name: 'Altavoz' })).toHaveAttribute(
    'aria-pressed',
    'false',
  )
  await dialog.getByRole('button', { name: 'Colgar' }).click()
  await expect(dialog).toHaveCount(0)
  expect(
    await page.evaluate(() => (window as unknown as { tracksStopped: number }).tracksStopped),
  ).toBeGreaterThan(0)
  await expect(page.locator('.message-user')).toHaveCount(1)
  await expect(page.locator('.message-user')).toContainText('Hello from the microphone')
})

test('barge-in stops playback and starts the next utterance', async ({ page }) => {
  await controlEnergy(page)
  const chat = await mockChat(page)
  let transcriptions = 0
  await page.route('**/blobs', (route) => route.fulfill({ json: { ref: 'input' } }))
  await page.route('**/blobs/output', (route) =>
    route.fulfill({ contentType: 'audio/wav', body: wav(10) }),
  )
  await page.route('**/rpc', async (route) => {
    const request = route.request().postDataJSON()
    let result
    if (request.method === 'voice.status') result = { available: true, voices: [] }
    if (request.method === 'voice.transcribe')
      result = { text: `Utterance ${++transcriptions}`, language: 'en', duration_ms: 1000 }
    if (request.method === 'voice.speak') result = { audio_blob: 'output', mime: 'audio/wav' }
    if (!result) return route.fallback()
    await route.fulfill({ json: { jsonrpc: '2.0', id: request.id, result } })
  })
  await page.goto('/agents/excalibur?view=chat')
  await page.getByRole('button', { name: 'Llamar', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'Llamada con Excalibur' })
  await expect(dialog.getByText('Escuchando', { exact: true })).toBeVisible()
  await energy(page, 0.1)
  await page.waitForTimeout(350)
  await energy(page, 0)
  await expect(dialog.getByText('Pensando', { exact: true })).toBeVisible()
  chat.emit('message.created', {
    message_id: 'answer-one',
    conversation_id: 'cnv_web_excalibur',
    role: 'assistant',
    text: 'A long spoken answer.',
  })
  await expect
    .poll(() =>
      page.evaluate(() =>
        performance
          .getEntriesByType('resource')
          .some((entry) => entry.name.endsWith('/blobs/output')),
      ),
    )
    .toBe(true)
  await energy(page, 0.1)
  await expect
    .poll(() => page.evaluate(() => (window as unknown as { pauses: number }).pauses))
    .toBeGreaterThan(0)
  await page.waitForTimeout(350)
  await energy(page, 0)
  await expect.poll(() => transcriptions).toBe(2)
  await dialog.getByRole('button', { name: 'Colgar' }).click()
})

test('unavailable voice and denied microphone permission have clear states', async ({ page }) => {
  let available = false
  await page.route('**/rpc', async (route) => {
    const request = route.request().postDataJSON()
    if (request.method !== 'voice.status') return route.fallback()
    await route.fulfill({
      json: {
        jsonrpc: '2.0',
        id: request.id,
        result: { available, voices: [], reason: 'Unconfigured' },
      },
    })
  })
  await page.addInitScript(() => {
    navigator.mediaDevices.getUserMedia = () =>
      Promise.reject(new DOMException('Denied', 'NotAllowedError'))
    // No browser speech either: nothing can carry the call.
    const w = window as unknown as Record<string, unknown>
    delete w.SpeechRecognition
    delete w.webkitSpeechRecognition
  })
  await page.goto('/agents/excalibur?view=chat')
  await page.getByRole('button', { name: 'Llamar', exact: true }).click()
  let dialog = page.getByRole('dialog', { name: 'Llamada con Excalibur' })
  await expect(dialog).toContainText('Las llamadas de voz no están disponibles.')
  await dialog.getByRole('button', { name: 'Colgar' }).click()
  await expect(dialog).toHaveCount(0)
  available = true
  await page.getByRole('button', { name: 'Llamar', exact: true }).click()
  dialog = page.getByRole('dialog', { name: 'Llamada con Excalibur' })
  await expect(dialog.getByRole('alert')).toContainText('Permite el acceso al micrófono')
})

test('without voice credentials the call uses browser speech, waits for a 3 s pause and reads clean text', async ({
  page,
}) => {
  await controlEnergy(page)
  await page.addInitScript(() => {
    const state = window as unknown as {
      recognizers: { onresult?: (e: unknown) => void; started: boolean }[]
      spoken: string[]
      cancels: number
      say: (text: string, final: boolean) => void
    }
    state.recognizers = []
    state.spoken = []
    state.cancels = 0
    class FakeRecognition {
      lang = ''
      continuous = false
      interimResults = false
      started = false
      onresult: ((e: unknown) => void) | null = null
      onerror: ((e: unknown) => void) | null = null
      onend: (() => void) | null = null
      constructor() {
        state.recognizers.push(this as never)
      }
      start() {
        this.started = true
      }
      stop() {
        this.started = false
        this.onend?.()
      }
      abort() {
        this.started = false
        this.onend?.()
      }
    }
    state.say = (text, final) => {
      const live = [...state.recognizers].reverse().find((r) => r.started)
      live?.onresult?.({
        resultIndex: 0,
        results: [Object.assign([{ transcript: text }], { isFinal: final })],
      })
    }
    const w = window as unknown as Record<string, unknown>
    w.SpeechRecognition = FakeRecognition
    w.webkitSpeechRecognition = FakeRecognition
    let current: { onend?: () => void; onerror?: (e: unknown) => void } | undefined
    Object.defineProperty(window, 'speechSynthesis', {
      configurable: true,
      value: {
        getVoices: () => [],
        speak(utterance: { text: string; onend?: () => void }) {
          state.spoken.push(utterance.text)
          current = utterance
        },
        cancel() {
          state.cancels++
          current?.onerror?.({ error: 'interrupted' })
          current = undefined
        },
        finish() {
          const done = current
          current = undefined
          done?.onend?.()
        },
      },
    })
  })
  const chat = await mockChat(page)
  await page.route('**/rpc', async (route) => {
    const request = route.request().postDataJSON()
    if (request.method !== 'voice.status') return route.fallback()
    await route.fulfill({
      json: { jsonrpc: '2.0', id: request.id, result: { available: false, voices: [] } },
    })
  })
  const spoken = () => page.evaluate(() => (window as unknown as { spoken: string[] }).spoken)
  const finishSentence = () =>
    page.evaluate(() => (window.speechSynthesis as unknown as { finish: () => void }).finish())
  await page.goto('/agents/excalibur?view=chat')
  await page.getByRole('button', { name: 'Llamar', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'Llamada con Excalibur' })
  await expect(dialog).not.toContainText('navegador')
  await expect(dialog.getByText('Escuchando', { exact: true })).toBeVisible()

  // A peak opens the turn; recognized words appear live.
  await energy(page, 0.1)
  await page.evaluate(() =>
    (window as unknown as { say: (t: string, f: boolean) => void }).say('hola cómo', false),
  )
  await expect(dialog.getByRole('log')).toContainText('hola cómo')
  await page.waitForTimeout(300)
  await energy(page, 0)
  await page.evaluate(() =>
    (window as unknown as { say: (t: string, f: boolean) => void }).say('hola cómo estás', true),
  )
  // A 2 s pause is not the end of the turn.
  await page.waitForTimeout(2000)
  expect(chat.messages.filter((m) => m.text === 'hola cómo estás')).toHaveLength(0)
  await expect(dialog.getByText('Pensando', { exact: true })).toBeVisible({ timeout: 3000 })
  expect(chat.messages.at(-1)?.text).toBe('hola cómo estás')

  chat.emit('message.created', {
    message_id: 'assistant-browser-voice',
    conversation_id: 'cnv_web_excalibur',
    role: 'assistant',
    text: '## Hola 👋\n\n- **Bien**, gracias.\n- Mira [esto](https://example.com).',
  })
  await expect.poll(spoken).toEqual(['Hola.'])
  await finishSentence()
  await expect.poll(spoken).toEqual(['Hola.', 'Bien, gracias.'])

  // Speaking over the assistant stops its voice at once.
  await energy(page, 0.1)
  await expect
    .poll(() => page.evaluate(() => (window as unknown as { cancels: number }).cancels))
    .toBeGreaterThan(0)
  await expect(dialog.getByText('Escuchando', { exact: true })).toBeVisible()
  await energy(page, 0)
  await dialog.getByRole('button', { name: 'Colgar' }).click()
  await expect(dialog).toHaveCount(0)
  expect(await spoken()).not.toContain('Mira esto.')
})

test('browser-voice call keeps hearing the user after the first spoken reply (two full turns)', async ({
  page,
}) => {
  await controlEnergy(page)
  await page.addInitScript(() => {
    const state = window as unknown as {
      recognizers: { onresult?: (e: unknown) => void; started: boolean }[]
      spoken: string[]
      cancels: number
      say: (text: string, final: boolean) => void
    }
    state.recognizers = []
    state.spoken = []
    state.cancels = 0
    class FakeRecognition {
      lang = ''
      continuous = false
      interimResults = false
      started = false
      onresult: ((e: unknown) => void) | null = null
      onerror: ((e: unknown) => void) | null = null
      onend: (() => void) | null = null
      constructor() {
        state.recognizers.push(this as never)
      }
      start() {
        this.started = true
      }
      stop() {
        this.started = false
        this.onend?.()
      }
      abort() {
        this.started = false
        this.onend?.()
      }
    }
    state.say = (text, final) => {
      const live = [...state.recognizers].reverse().find((r) => r.started)
      live?.onresult?.({
        resultIndex: 0,
        results: [Object.assign([{ transcript: text }], { isFinal: final })],
      })
    }
    const w = window as unknown as Record<string, unknown>
    w.SpeechRecognition = FakeRecognition
    w.webkitSpeechRecognition = FakeRecognition
    let current: { onend?: () => void; onerror?: (e: unknown) => void } | undefined
    Object.defineProperty(window, 'speechSynthesis', {
      configurable: true,
      value: {
        getVoices: () => [],
        speak(utterance: { text: string; onend?: () => void }) {
          state.spoken.push(utterance.text)
          current = utterance
        },
        cancel() {
          state.cancels++
          current?.onerror?.({ error: 'interrupted' })
          current = undefined
        },
        finish() {
          const done = current
          current = undefined
          done?.onend?.()
        },
      },
    })
  })
  const chat = await mockChat(page)
  await page.route('**/rpc', async (route) => {
    const request = route.request().postDataJSON()
    if (request.method !== 'voice.status') return route.fallback()
    await route.fulfill({
      json: { jsonrpc: '2.0', id: request.id, result: { available: false, voices: [] } },
    })
  })
  const spoken = () => page.evaluate(() => (window as unknown as { spoken: string[] }).spoken)
  const finishSentence = () =>
    page.evaluate(() => (window.speechSynthesis as unknown as { finish: () => void }).finish())
  const say = (text: string, final: boolean) =>
    page.evaluate(
      ([t, f]) => (window as unknown as { say: (t: string, f: boolean) => void }).say(t, f),
      [text, final] as const,
    )
  const finishAll = async () => {
    for (let i = 0; i < 10; i++)
      await page.evaluate(() =>
        (window.speechSynthesis as unknown as { finish: () => void }).finish(),
      )
  }
  await page.goto('/agents/excalibur?view=chat')
  await page.getByRole('button', { name: 'Llamar', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'Llamada con Excalibur' })
  await expect(dialog.getByText('Escuchando', { exact: true })).toBeVisible()

  for (const [index, words] of ['primer mensaje', 'segundo mensaje', 'tercer mensaje'].entries()) {
    await energy(page, 0.1)
    await say(words, false)
    await page.waitForTimeout(300)
    await energy(page, 0)
    await say(words, true)
    await expect.poll(() => chat.messages.at(-1)?.text, { timeout: 6000 }).toBe(words)
    chat.emit('message.created', {
      message_id: `reply-${index}`,
      conversation_id: 'cnv_web_excalibur',
      role: 'assistant',
      text: `Respuesta ${index + 1}. Con dos frases.`,
    })
    await expect.poll(spoken).toContain(`Respuesta ${index + 1}.`)
    await finishAll()
    await expect(dialog.getByText('Escuchando', { exact: true })).toBeVisible()
  }
  // While the reply plays, the recognizer hears our own voice (ignored) and then the user (barge-in).
  await energy(page, 0.1)
  await say('cuéntame algo largo', true)
  await page.waitForTimeout(300)
  await energy(page, 0)
  await expect.poll(() => chat.messages.at(-1)?.text, { timeout: 6000 }).toBe('cuéntame algo largo')
  chat.emit('message.created', {
    message_id: 'reply-long',
    conversation_id: 'cnv_web_excalibur',
    role: 'assistant',
    text: 'Esta es una respuesta larga. Tiene varias frases. Y sigue hablando un buen rato.',
  })
  await expect.poll(spoken).toContain('Esta es una respuesta larga.')
  await say('esta es una respuesta larga', false)
  await page.waitForTimeout(500)
  await expect(dialog.getByText('Hablando', { exact: true })).toBeVisible()
  await say('esta es una respuesta larga espera quiero preguntarte otra cosa', false)
  await expect(dialog.getByText('Escuchando', { exact: true })).toBeVisible()
  await say('esta es una respuesta larga espera quiero preguntarte otra cosa', true)
  await expect
    .poll(() => chat.messages.at(-1)?.text, { timeout: 6000 })
    .toBe('espera quiero preguntarte otra cosa')
  expect(chat.messages.filter((m) => m.role === 'user').map((m) => m.text)).toEqual([
    'primer mensaje',
    'segundo mensaje',
    'tercer mensaje',
    'cuéntame algo largo',
    'espera quiero preguntarte otra cosa',
  ])
  await dialog.getByRole('button', { name: 'Colgar' }).click()
})
