import '@/lib/i18n'
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import type { Frame } from './types'
const mocks = vi.hoisted(() => ({
  call: vi.fn(),
  frame: undefined as ((frame: Frame) => void) | undefined,
}))
vi.mock('./api', () => ({
  labCall: mocks.call,
  useRobotEvent: (_type: string, callback: (frame: Frame) => void) => {
    mocks.frame = callback
  },
}))
import { SceneRunControls } from './SceneRunControls'
import { useSceneRuns } from './sceneRun'
afterEach(() => {
  cleanup()
  vi.useRealTimers()
  vi.resetAllMocks()
  useSceneRuns.setState({
    session: undefined,
    playback: undefined,
    run: { mode: 'idle', previous: 'idle', received: 0, pending: false },
  })
})
function mount() {
  mocks.call.mockImplementation(async (_method, params) => ({ mode: params.mode }))
  render(<SceneRunControls session="s" shortcut />)
}
it('pauses and restores the last authoritative driving mode', async () => {
  mount()
  act(() => mocks.frame!({ session: 's', mode: 'manual' } as Frame))
  await act(async () => fireEvent.click(screen.getByRole('button')))
  expect(screen.getByRole('status')).toHaveAttribute('data-state', 'paused')
  expect(mocks.call).toHaveBeenLastCalledWith('robotlab.session.control', {
    session: 's',
    mode: 'paused',
  })
  await act(async () => fireEvent.click(screen.getByRole('button')))
  expect(mocks.call).toHaveBeenLastCalledWith('robotlab.session.control', {
    session: 's',
    mode: 'manual',
  })
  expect(screen.getByRole('status')).toHaveAttribute('data-state', 'running')
})
it('shows signal loss after two seconds and keeps paused sessions paused', async () => {
  vi.useFakeTimers()
  mount()
  act(() => vi.advanceTimersByTime(2250))
  expect(screen.getByRole('status')).toHaveAttribute('data-state', 'noSignal')
  act(() => mocks.frame!({ session: 's', mode: 'policy' } as Frame))
  expect(screen.getByRole('status')).toHaveAttribute('data-state', 'running')
  await act(async () => fireEvent.click(screen.getByRole('button')))
  act(() => vi.advanceTimersByTime(3000))
  expect(screen.getByRole('status')).toHaveAttribute('data-state', 'paused')
})
it('uses P without intercepting Space or text editing', async () => {
  mount()
  fireEvent.keyDown(document.body, { code: 'Space' })
  expect(mocks.call).not.toHaveBeenCalled()
  const input = document.createElement('input')
  document.body.append(input)
  fireEvent.keyDown(input, { code: 'KeyP' })
  expect(mocks.call).not.toHaveBeenCalled()
  input.remove()
  await act(async () => fireEvent.keyDown(document.body, { code: 'KeyP' }))
  expect(screen.getByRole('status')).toHaveAttribute('data-state', 'paused')
})
it('keeps the server state on a failed pause and ignores preview frames', async () => {
  const error = vi.fn()
  mocks.call.mockRejectedValue(new Error('offline'))
  render(<SceneRunControls session="s" onError={error} />)
  act(() => mocks.frame!({ session: 's', preview: true, mode: 'paused' } as Frame))
  await act(async () => fireEvent.click(screen.getByRole('button')))
  expect(error).toHaveBeenCalledOnce()
  expect(screen.getByRole('status')).toHaveAttribute('data-state', 'running')
  expect(screen.getByRole('button')).toBeEnabled()
})
