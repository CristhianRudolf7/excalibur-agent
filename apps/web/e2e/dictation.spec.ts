import { test, expect } from './fixtures'
import { mockChat } from './chat-fixture'

test('composer microphone dictates with browser speech and never repeats growing results', async ({
  page,
}) => {
  await page.addInitScript(() => {
    const state = window as unknown as {
      recognizers: { started: boolean; onresult?: (e: unknown) => void }[]
      say: (...texts: string[]) => void
    }
    state.recognizers = []
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
    // Android-style: every growing prefix arrives as another final result.
    state.say = (...texts) => {
      const live = [...state.recognizers].reverse().find((r) => r.started)
      live?.onresult?.({
        resultIndex: texts.length - 1,
        results: texts.map((transcript) => Object.assign([{ transcript }], { isFinal: true })),
      })
    }
    const w = window as unknown as Record<string, unknown>
    w.SpeechRecognition = FakeRecognition
    w.webkitSpeechRecognition = FakeRecognition
  })
  await mockChat(page)
  await page.goto('/agents/excalibur?view=chat')
  const box = page.getByRole('textbox', { name: /Escribe a Excalibur/ })
  await box.fill('Nota:')
  const mic = page.getByRole('button', { name: 'Dictar por voz' })
  await mic.click()
  await expect(page.getByRole('button', { name: 'Detener dictado' })).toHaveAttribute(
    'aria-pressed',
    'true',
  )
  await page.evaluate(() =>
    (window as unknown as { say: (...t: string[]) => void }).say(
      'tengo',
      'tengo',
      'tengo algún',
      'tengo algún listado de tarea pendiente',
    ),
  )
  await expect(box).toHaveValue('Nota: tengo algún listado de tarea pendiente')
  await page.getByRole('button', { name: 'Detener dictado' }).click()
  await expect(page.getByRole('button', { name: 'Dictar por voz' })).toBeVisible()
  await expect(page.getByText('Disponible próximamente')).toHaveCount(0)
})
