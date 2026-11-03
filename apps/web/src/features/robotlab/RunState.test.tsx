import '@/lib/i18n'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { RunStatus, RunToggle, type RunState } from './RunState'
afterEach(cleanup)
it.each<RunState>([
  'running',
  'paused',
  'stopped',
  'starting',
  'stopping',
  'done',
  'error',
  'noSignal',
])('announces %s with text and an icon', (state) => {
  render(<RunStatus state={state} />)
  const pill = screen.getByRole('status')
  expect(pill).toHaveAttribute('aria-live', 'polite')
  expect(pill).toHaveAttribute('data-state', state)
  expect(pill.textContent!.length).toBeGreaterThan(2)
  expect(pill.querySelector('svg')).toHaveAttribute('aria-hidden', 'true')
})
it('uses surface labels and includes training progress', () => {
  render(<RunStatus state="running" surface="experiment" progress="20 / 100 · 20%" />)
  expect(screen.getByRole('status')).toHaveTextContent(/Training|Entrenando/)
  expect(screen.getByRole('status')).toHaveTextContent('20%')
})
it('exposes the pressed state and blocks duplicate actions while busy', () => {
  const click = vi.fn()
  const view = render(<RunToggle active label="Pause simulation" onClick={click} />)
  fireEvent.click(screen.getByRole('button'))
  expect(click).toHaveBeenCalledOnce()
  expect(screen.getByRole('button')).toHaveAttribute('aria-pressed', 'true')
  view.rerender(<RunToggle active={false} busy label="Starting…" onClick={click} />)
  expect(screen.getByRole('button')).toBeDisabled()
  fireEvent.click(screen.getByRole('button'))
  expect(click).toHaveBeenCalledOnce()
})
