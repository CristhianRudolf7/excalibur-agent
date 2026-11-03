import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import i18n from '@/lib/i18n'
import { MessageSpeak, speakableText } from './MessageSpeak'

let utterances: { text: string; onend?: () => void }[] = []
beforeEach(async () => {
  await i18n.changeLanguage('es')
  utterances = []
  class Utterance {
    text: string
    lang = ''
    voice: unknown = null
    onend?: () => void
    onerror?: () => void
    constructor(text: string) {
      this.text = text
      utterances.push(this)
    }
  }
  vi.stubGlobal('SpeechSynthesisUtterance', Utterance)
  vi.stubGlobal('speechSynthesis', { speak: vi.fn(), cancel: vi.fn(), getVoices: () => [] })
})
afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

it('reads a message aloud and turns into a red stop control while speaking', () => {
  render(<MessageSpeak id="m1" text={'**Hola** `mundo` [enlace](http://x)'} />)
  fireEvent.click(screen.getByRole('button', { name: 'Escuchar mensaje' }))
  expect(window.speechSynthesis.speak).toHaveBeenCalledOnce()
  expect(utterances[0]!.text).toBe('Hola mundo enlace.')
  const stop = screen.getByRole('button', { name: 'Detener lectura' })
  expect(stop).toHaveClass('speaking')
  fireEvent.click(stop)
  expect(window.speechSynthesis.cancel).toHaveBeenCalled()
  expect(screen.getByRole('button', { name: 'Escuchar mensaje' })).not.toHaveClass('speaking')
})

it('returns to idle when speech ends', () => {
  render(<MessageSpeak id="m2" text="Texto" />)
  fireEvent.click(screen.getByRole('button', { name: 'Escuchar mensaje' }))
  act(() => utterances[0]!.onend?.())
  expect(screen.getByRole('button', { name: 'Escuchar mensaje' })).toBeInTheDocument()
})

it('strips code blocks from speech', () => {
  expect(speakableText('Mira:\n```js\nconst a = 1\n```\nListo')).toBe('Mira: Listo.')
})
