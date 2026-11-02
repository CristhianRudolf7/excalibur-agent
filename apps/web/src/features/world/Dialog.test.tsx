import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import '@/lib/i18n'
import { DialogQueue, RpgDialog } from './Dialog'
import { agents } from './seed/agents'
import { initialDialog } from './seed/timeline'
const event = initialDialog(1000),
  identities = Object.fromEntries(agents.map((agent) => [agent.id, agent]))
beforeEach(() => {
  vi.useFakeTimers()
  vi.stubGlobal('matchMedia', () => ({ matches: false }))
})
afterEach(() => {
  cleanup()
  vi.useRealTimers()
  vi.unstubAllGlobals()
})
it('reveals full text before calling the supplied advance callback', () => {
  const advance = vi.fn()
  render(<RpgDialog event={event} agent={agents[0]} count={3} onAdvance={advance} />)
  fireEvent.click(screen.getByRole('button'))
  expect(advance).not.toHaveBeenCalled()
  expect(screen.getByRole('button').textContent).toContain('equipo')
  fireEvent.click(screen.getByRole('button'))
  expect(advance).toHaveBeenCalledOnce()
})
it('pauses auto-dismiss while hovered and resumes after leaving', () => {
  const advance = vi.fn()
  render(<DialogQueue messages={[event]} agents={identities} onAdvance={advance} />)
  fireEvent.mouseEnter(screen.getByRole('region'))
  act(() => vi.advanceTimersByTime(9000))
  expect(advance).not.toHaveBeenCalled()
  fireEvent.mouseLeave(screen.getByRole('region'))
  act(() => vi.advanceTimersByTime(8000))
  expect(advance).toHaveBeenCalledOnce()
})
it('shows complete text immediately with reduced motion and supports an empty queue', () => {
  vi.stubGlobal('matchMedia', () => ({ matches: true }))
  const advance = vi.fn(),
    view = render(<DialogQueue messages={[event]} agents={identities} onAdvance={advance} />)
  expect(screen.getByRole('button').textContent).toContain('equipo')
  fireEvent.click(screen.getByRole('button'))
  expect(advance).toHaveBeenCalledOnce()
  view.rerender(<DialogQueue messages={[]} agents={identities} onAdvance={advance} />)
  expect(screen.queryByRole('region')).toBeNull()
})
