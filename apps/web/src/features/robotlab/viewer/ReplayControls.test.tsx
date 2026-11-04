import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import i18n from '@/lib/i18n'
import { ReplayControls } from './ReplayControls'
const props = {
  frame: null,
  t: 0.25,
  duration: 2,
  playing: false,
  loading: false,
  play: vi.fn(),
  pause: vi.fn(),
  seek: vi.fn(),
  speed: 1,
  setSpeed: vi.fn(),
}
afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})
it('offers accessible playback, scrubbing and speed controls in both languages', async () => {
  await i18n.changeLanguage('en')
  const view = render(<ReplayControls {...props} label="Attempt 1" />)
  expect(screen.getByRole('group', { name: 'Attempt 1' })).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'Play' }))
  expect(props.play).toHaveBeenCalledOnce()
  fireEvent.change(screen.getByRole('slider', { name: 'Replay position' }), {
    target: { value: '1.5' },
  })
  expect(props.seek).toHaveBeenCalledWith(1.5)
  fireEvent.change(screen.getByRole('combobox', { name: 'Playback speed' }), {
    target: { value: '2' },
  })
  expect(props.setSpeed).toHaveBeenCalledWith(2)
  view.rerender(<ReplayControls {...props} playing />)
  fireEvent.click(screen.getByRole('button', { name: 'Pause' }))
  expect(props.pause).toHaveBeenCalledOnce()
  await act(() => i18n.changeLanguage('es'))
  expect(screen.getByRole('button', { name: 'Pausar' })).toBeInTheDocument()
  expect(screen.getByRole('slider', { name: 'Posición de la repetición' })).toBeInTheDocument()
  expect(screen.getByRole('combobox', { name: 'Velocidad de reproducción' })).toBeInTheDocument()
})
it('disables playback while loading or empty and exposes errors', () => {
  const view = render(<ReplayControls {...props} loading />)
  expect(screen.getByRole('button')).toBeDisabled()
  expect(screen.getByRole('slider')).toBeDisabled()
  expect(screen.getByRole('status')).toBeInTheDocument()
  view.rerender(<ReplayControls {...props} duration={0} />)
  expect(screen.getByRole('button')).toBeDisabled()
  view.rerender(<ReplayControls {...props} error="Could not load" />)
  expect(screen.getByRole('alert')).toHaveTextContent('Could not load')
})
