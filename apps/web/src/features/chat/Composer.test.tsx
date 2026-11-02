import { fireEvent, render, screen, cleanup } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import '@/lib/i18n'
import { Composer } from './Composer'
import { composerHeight, shouldSend } from './composer-logic'
afterEach(cleanup)
describe('composer', () => {
  it('clamps height by viewport and maximum', () => {
    expect(composerHeight(900, 900)).toBe(240)
    expect(composerHeight(900, 390)).toBe(156)
    expect(composerHeight(2, 900)).toBe(24)
  })
  it('sends Enter, preserves Shift+Enter, and guards IME input', () => {
    expect(
      shouldSend({
        key: 'Enter',
        shiftKey: false,
        nativeEvent: { isComposing: true } as KeyboardEvent,
      }),
    ).toBe(false)
    const send = vi.fn()
    render(<Composer agent="Byte" typing={false} onSend={send} onStop={() => {}} />)
    const input = screen.getByRole('textbox')
    fireEvent.change(input, { target: { value: 'Hello' } })
    fireEvent.keyDown(input, { key: 'Enter', shiftKey: true })
    expect(send).not.toHaveBeenCalled()
    fireEvent.keyDown(input, { key: 'Enter', isComposing: true })
    expect(send).not.toHaveBeenCalled()
    fireEvent.keyDown(input, { key: 'Enter' })
    expect(send).toHaveBeenCalledWith('Hello')
    expect(input).toHaveValue('')
    expect(input).toHaveFocus()
  })
  it('disables empty send and stops a pending response', () => {
    const stop = vi.fn()
    const { rerender } = render(
      <Composer agent="Byte" typing={false} onSend={() => {}} onStop={stop} />,
    )
    expect(screen.getByRole('button', { name: 'Enviar mensaje' })).toBeDisabled()
    rerender(<Composer agent="Byte" typing onSend={() => {}} onStop={stop} />)
    fireEvent.click(screen.getByRole('button', { name: 'Detener respuesta' }))
    expect(stop).toHaveBeenCalledOnce()
  })
})

it('passes explicit attachment IDs to the connected chat callback', () => {
  const connected = vi.fn(),
    preview = vi.fn()
  render(
    <Composer
      agent="Byte"
      typing={false}
      onSend={preview}
      onSendDocuments={connected}
      onStop={() => {}}
    />,
  )
  fireEvent.change(screen.getByRole('textbox'), { target: { value: 'Hello daemon' } })
  fireEvent.keyDown(screen.getByRole('textbox'), { key: 'Enter' })
  expect(connected).toHaveBeenCalledWith('Hello daemon', [])
  expect(preview).not.toHaveBeenCalled()
})
