import type { WebSocketRoute } from '@playwright/test'
import { test, expect, type Page } from './fixtures'
import { mockChat } from './chat-fixture'
import { mockEvents } from './event-fixture'

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

async function setup(page: Page, unavailable = false) {
  await page.addInitScript(() => {
    const state = window as unknown as {
      micEnergy: number
      say: (text: string) => void
      spoken: string[]
    }
    state.micEnergy = 0
    state.spoken = []
    const create = AudioContext.prototype.createAnalyser
    AudioContext.prototype.createAnalyser = function () {
      const analyser = create.call(this)
      analyser.getFloatTimeDomainData = (samples: Float32Array<ArrayBuffer>) =>
        samples.fill(state.micEnergy)
      return analyser
    }
    let recognition: { onresult?: (event: unknown) => void } | undefined
    class Recognition {
      onresult?: (event: unknown) => void
      start() {
        recognition = this
      }
      abort() {
        if (recognition === this) recognition = undefined
      }
    }
    state.say = (text) =>
      recognition?.onresult?.({
        resultIndex: 0,
        results: [{ isFinal: true, 0: { transcript: text } }],
      })
    const w = window as unknown as Record<string, unknown>
    w.SpeechRecognition = Recognition
    w.webkitSpeechRecognition = Recognition
    Object.defineProperty(window, 'speechSynthesis', {
      configurable: true,
      value: {
        getVoices: () => [],
        cancel() {},
        speak(utterance: { text: string; onend?: () => void }) {
          state.spoken.push(utterance.text)
          setTimeout(() => utterance.onend?.(), 10)
        },
      },
    })
  })
  const chat = await mockChat(page)
  const bus = await mockEvents(page)
  let listen: WebSocketRoute | undefined
  let binaryFrames = 0
  let commits = 0
  let closed = 0
  let final = 'Hello from live voice.'
  const speech: { id: number; text: string }[] = []
  await page.route('**/rpc', async (route) => {
    const request = route.request().postDataJSON()
    if (request.method !== 'voice.status') return route.fallback()
    await route.fulfill({
      json: { jsonrpc: '2.0', id: request.id, result: { available: true, voices: [] } },
    })
  })
  await page.routeWebSocket('**/ws/voice/listen**', (socket) => {
    listen = socket
    expect(new URL(socket.url()).searchParams.get('language')).toBe('es')
    socket.onClose(() => closed++)
    socket.onMessage((data) => {
      if (typeof data !== 'string') {
        expect(data.byteLength).toBe(4800)
        binaryFrames++
        return
      }
      const event = JSON.parse(data)
      if (event.type === 'commit') {
        commits++
        socket.send(JSON.stringify({ type: 'final', item: `turn-${commits}`, text: final }))
      }
    })
    socket.send(
      JSON.stringify(unavailable ? { type: 'error', code: 'unavailable' } : { type: 'ready' }),
    )
  })
  await page.routeWebSocket('**/ws/voice/speak', (socket) => {
    socket.onClose(() => closed++)
    socket.onMessage((data) => {
      const event = JSON.parse(String(data))
      if (event.type !== 'speak') return
      speech.push({ id: event.id, text: event.text })
      const pcm = Buffer.alloc(4 + 2400 * 2)
      pcm.writeUInt32BE(event.id)
      socket.send(pcm)
      socket.send(JSON.stringify({ type: 'done', id: event.id }))
    })
  })
  await page.goto('/agents/excalibur?view=chat')
  await bus.ready('browser.session')
  if ((page.viewportSize()?.width ?? 1280) < 640) {
    await page.getByRole('button', { name: 'Más opciones' }).click()
    await page.getByRole('menuitem', { name: 'Llamar', exact: true }).click()
  } else await page.getByRole('button', { name: 'Llamar', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'Llamada con Excalibur' })
  await expect(dialog.getByText('Escuchando', { exact: true })).toBeVisible()
  return {
    chat,
    bus,
    dialog,
    speech,
    binaryFrames: () => binaryFrames,
    commits: () => commits,
    closed: () => closed,
    delta(text: string, item = 'turn-1') {
      listen!.send(JSON.stringify({ type: 'delta', item, text }))
    },
    setFinal(text: string) {
      final = text
    },
  }
}
const energy = (page: Page, value: number) =>
  page.evaluate((level) => {
    ;(window as unknown as { micEnergy: number }).micEnergy = level
  }, value)

test('a fake microphone streams a complete call turn with partial captions and streamed reply audio', async ({
  page,
}) => {
  const voice = await setup(page)
  await energy(page, 0.1)
  await expect.poll(voice.binaryFrames).toBeGreaterThan(0)
  voice.delta('Hello from')
  await expect(voice.dialog.locator('.call-line-partial')).toHaveText('Hello from')
  voice.delta(' live voice')
  await expect(voice.dialog.locator('.call-line-partial')).toHaveText('Hello from live voice')
  await energy(page, 0)
  await expect.poll(() => voice.chat.messages.at(-1)?.text).toBe('Hello from live voice.')
  expect(voice.commits()).toBe(1)
  voice.chat.emit('message.created', {
    message_id: 'streamed-reply',
    conversation_id: 'cnv_web_excalibur',
    role: 'assistant',
    text: 'Here is the answer. Two sentences together.',
  })
  await expect
    .poll(() => voice.speech.map((request) => request.text))
    .toEqual(['Here is the answer. Two sentences together.'])
  await expect(voice.dialog.getByText('Escuchando', { exact: true })).toBeVisible()
  await expect(voice.dialog.getByRole('alert')).toHaveCount(0)
  await voice.dialog.getByRole('button', { name: 'Colgar' }).click()
  await expect.poll(voice.closed).toBe(2)
})

test('listen unavailable falls back to browser speech without an alert and keeps the call alive', async ({
  page,
}) => {
  const voice = await setup(page, true)
  await expect(voice.dialog.getByText('Escuchando', { exact: true })).toBeVisible()
  // Nothing was said yet, so switching at the start of a call stays silent.
  await expect(voice.dialog.locator('.call-notice')).toHaveCount(0)
  await expect(voice.dialog.getByRole('alert')).toHaveCount(0)
  await page.evaluate(() =>
    (window as unknown as { say: (text: string) => void }).say('Browser fallback words'),
  )
  await expect(voice.dialog.locator('.call-line-partial')).toHaveText('Browser fallback words')
  await expect
    .poll(() => voice.chat.messages.at(-1)?.text, { timeout: 6000 })
    .toBe('Browser fallback words')
  voice.chat.emit('message.created', {
    message_id: 'browser-reply',
    conversation_id: 'cnv_web_excalibur',
    role: 'assistant',
    text: 'Browser reply.',
  })
  await expect
    .poll(() => page.evaluate(() => (window as unknown as { spoken: string[] }).spoken))
    .toContain('Browser reply.')
  await expect(voice.dialog.getByText('Escuchando', { exact: true })).toBeVisible()
  expect(voice.speech).toEqual([])
  await voice.dialog.getByRole('button', { name: 'Colgar' }).click()
})

for (const width of [1280, 390]) {
  test(`browser activity minimizes the call and keeps compact captions contained at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 })
    let active = false
    const session = {
      conversation_id: 'cnv_web_excalibur',
      session_id: 'voice-browser',
      run_id: 'run',
      state: 'active',
      title: 'Voice browsing',
      url: 'https://example.test',
    }
    await page.route('**/rpc', async (route) => {
      const request = route.request().postDataJSON()
      if (request.method !== 'browser.live_state') return route.fallback()
      await route.fulfill({
        json: { jsonrpc: '2.0', id: request.id, result: active ? { session } : {} },
      })
    })
    await page.routeWebSocket('**/ws/browser**', () => {})
    const voice = await setup(page)
    await energy(page, 0.1)
    await expect.poll(voice.binaryFrames).toBeGreaterThan(0)
    const text =
      'Live caption with a very long phrase that stays inside the compact call bar. '.repeat(8)
    voice.delta(text)
    await expect(voice.dialog.locator('.call-line-partial')).toHaveText(text.trim())
    active = true
    voice.bus.emit('browser.session', session)
    await expect(page.locator('.browser-live')).toBeVisible()
    await expect(voice.dialog).toHaveCount(0)
    const bar = page.locator('.composer-call-bar')
    await expect(bar).toBeVisible()
    await expect(bar.locator('.composer-call-caption')).toHaveText(text.trim())
    await expect(bar.getByRole('button', { name: 'Colgar' })).toBeVisible()
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    expect(await bar.evaluate((node) => node.scrollWidth <= node.clientWidth)).toBe(true)
    if (width === 1280) {
      const call = await bar.boundingBox()
      const panel = await page.locator('.right-panel').boundingBox()
      expect(call!.x + call!.width).toBeLessThanOrEqual(panel!.x)
      await bar.getByRole('button', { name: 'Mostrar llamada' }).click()
      await expect(voice.dialog).toBeVisible()
      voice.bus.emit('credentials.requested', {
        conversation_id: 'foreign',
        request_id: 'foreign-request',
        run_id: 'run',
        fields: ['password'],
        site: 'https://example.test',
        purpose: 'Sign in',
        created_at: new Date().toISOString(),
      })
      await expect(voice.dialog).toBeVisible()
      voice.bus.emit('credentials.requested', {
        conversation_id: session.conversation_id,
        request_id: 'secure-request',
        run_id: 'run',
        fields: ['password'],
        site: 'https://example.test',
        purpose: 'Sign in',
        created_at: new Date().toISOString(),
      })
      await expect(voice.dialog).toHaveCount(0)
      await expect(page.getByLabel('Contraseña', { exact: true })).toBeVisible()
      await expect(bar).toBeVisible()
    }
  })
}
