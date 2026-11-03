import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import type { ScenarioInfo, Scene } from './types'
const call = vi.hoisted(() => vi.fn())
vi.mock('./api', () => ({ labCall: call, useRobotEvent: vi.fn() }))
vi.mock('./i18n', () => ({ useLabText: () => ({ text: (key: string) => key, language: 'en' }) }))
import { Controls } from './Controls'
import { useSceneRuns } from './sceneRun'
import { useLab } from './state'
const actuator = {
  name: 'motor',
  label: { en: 'Motor', es: 'Motor' },
  kind: 'torque' as const,
  range: [-10, 10] as [number, number],
  key_neg: 'KeyA',
  key_pos: 'KeyD',
}
const scene: Scene = {
  scenario: 'cartpole',
  params: {},
  timestep: 0.02,
  bodies: [],
  geoms: [],
  cameras: [],
  actuators: [actuator],
  camera_default: { pos: [2, 2, 2], target: [0, 0, 0] },
}
const scenario: ScenarioInfo = {
  id: 'cartpole',
  title: { en: 'Cartpole', es: 'Cartpole' },
  description: { en: '', es: '' },
  category: 'classic',
  params_schema: {},
  default_params: {},
  actuators: [actuator],
  reward_terms: [],
  algorithms: ['PPO'],
  default_total_steps: 10,
  inputs: ['sliders', 'keyboard', 'gamepad'],
  has_pretrained: true,
  has_vision: false,
  thumbnail_color: '#2f6bff',
}
function mount() {
  call.mockImplementation((_method: string, params: { mode?: string }) =>
    Promise.resolve({ mode: params.mode ?? 'policy' }),
  )
  return render(
    <Controls
      active={{ session: 'ses', scene }}
      scenario={scenario}
      onScene={vi.fn()}
      weights={{}}
      onWeights={vi.fn()}
      onError={vi.fn()}
    />,
  )
}
afterEach(() => {
  cleanup()
  vi.useRealTimers()
  vi.clearAllMocks()
  useLab.setState({ active: undefined })
  useSceneRuns.setState({
    session: undefined,
    run: { mode: 'idle', previous: 'idle', received: 0, pending: false },
  })
})
it('returns torque sliders to neutral after release and sends the trailing value', async () => {
  vi.useFakeTimers()
  mount()
  fireEvent.change(screen.getByRole('slider', { name: 'Motor' }), { target: { value: '0.7' } })
  await act(async () => {})
  expect(call).toHaveBeenLastCalledWith('robotlab.session.control', {
    session: 'ses',
    mode: 'manual',
    inputs: { motor: 0.7 },
  })
  fireEvent.pointerUp(screen.getByRole('slider', { name: 'Motor' }))
  await act(async () => {
    vi.advanceTimersByTime(34)
  })
  expect(call).toHaveBeenLastCalledWith('robotlab.session.control', {
    session: 'ses',
    mode: 'manual',
    inputs: { motor: 0 },
  })
})
it('maps actuator keys and releases held keys when the window loses focus', async () => {
  vi.useFakeTimers()
  mount()
  fireEvent.keyDown(document.body, { code: 'KeyD' })
  await act(async () => {})
  expect(call).toHaveBeenLastCalledWith('robotlab.session.control', {
    session: 'ses',
    mode: 'manual',
    inputs: { motor: 1 },
  })
  fireEvent.blur(window)
  await act(async () => {
    vi.advanceTimersByTime(34)
  })
  expect(call).toHaveBeenLastCalledWith('robotlab.session.control', {
    session: 'ses',
    mode: 'manual',
    inputs: { motor: 0 },
  })
})
it('samples gamepads only in manual mode and exposes the active input', async () => {
  vi.useFakeTimers()
  const original = Object.getOwnPropertyDescriptor(navigator, 'getGamepads')
  const getGamepads = vi.fn(() => [{ connected: true, axes: [0.6] }])
  Object.defineProperty(navigator, 'getGamepads', { configurable: true, value: getGamepads })
  try {
    mount()
    await act(async () => {
      vi.advanceTimersByTime(100)
    })
    expect(getGamepads).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: 'manual' }))
    await act(async () => {})
    await act(async () => {
      vi.advanceTimersByTime(34)
    })
    await act(async () => {
      vi.advanceTimersByTime(34)
    })
    expect(call).toHaveBeenLastCalledWith('robotlab.session.control', {
      session: 'ses',
      mode: 'manual',
      inputs: { motor: 0.6 },
    })
    expect(screen.getByText('gamepad')).toBeVisible()
  } finally {
    if (original) Object.defineProperty(navigator, 'getGamepads', original)
    else Reflect.deleteProperty(navigator, 'getGamepads')
  }
})

it('neutralizes manual input and cancels queued controls when leaving the scene', async () => {
  vi.useFakeTimers()
  useLab.setState({ active: { session: 'ses', scene } })
  const view = mount()
  fireEvent.change(screen.getByRole('slider', { name: 'Motor' }), { target: { value: '0.7' } })
  await act(async () => {})
  fireEvent.change(screen.getByRole('slider', { name: 'Motor' }), { target: { value: '0.9' } })
  view.unmount()
  expect(call).toHaveBeenLastCalledWith('robotlab.session.control', {
    session: 'ses',
    mode: 'manual',
    inputs: { motor: 0 },
  })
  const count = call.mock.calls.length
  await act(() => vi.advanceTimersByTime(100))
  expect(call).toHaveBeenCalledTimes(count)
})

it('choosing a controller while paused resumes in that mode', async () => {
  mount()
  await act(async () => fireEvent.click(screen.getByRole('button', { name: 'pauseSimulation' })))
  expect(screen.getByText('runPaused')).toBeVisible()
  expect(screen.queryByRole('button', { name: 'paused' })).not.toBeInTheDocument()
  await act(async () => fireEvent.click(screen.getByRole('button', { name: 'manual' })))
  expect(call).toHaveBeenLastCalledWith('robotlab.session.control', {
    session: 'ses',
    mode: 'manual',
  })
  expect(screen.getByText('runRunning')).toBeVisible()
})
