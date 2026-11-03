import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import '@/lib/i18n'
import type { Scene, ScenarioInfo } from './types'
import { useLab } from './state'
const mocks = vi.hoisted(() => ({ call: vi.fn(), scenario: {} }))
vi.mock('@tanstack/react-query', () => ({
  useQuery: ({ queryKey }: { queryKey: string[] }) => ({
    data: queryKey[1] === 'status' ? { installed: true } : { scenarios: [mocks.scenario] },
  }),
}))
vi.mock('./api', () => ({ labCall: mocks.call, robotlabEnabled: true, useRobotEvent: vi.fn() }))
vi.mock('./i18n', () => ({
  robotlabMessages: {},
  useLabText: () => ({ text: (key: string) => key, language: 'en' }),
}))
vi.mock('@/features/chat/Chat', () => ({
  Chat: ({ agentId }: { agentId: string }) => <div>Chat {agentId}</div>,
}))
vi.mock('./Controls', () => ({ Controls: () => null }))
vi.mock('./ToolStore', () => ({ ToolStore: () => null }))
vi.mock('./Viewer', () => ({
  Viewer: ({ session }: { session: string }) => <div>Viewer {session}</div>,
}))
vi.mock('./vision/VisionPanel', () => ({
  VisionPanel: ({ session }: { session: string }) => <div>Vision {session}</div>,
}))
vi.mock('./teleop/TeleopPanel', () => ({
  TeleopPanel: ({
    session,
    scene,
    viewer,
    sendPose,
  }: import('./teleop/TeleopPanel').TeleopPanelProps) => (
    <div>
      {session} {scene.scenario}
      {viewer}
      <button onClick={() => sendPose({ landmarks: [], t: performance.now() })}>Send pose</button>
    </div>
  ),
}))
import RobotLab from './RobotLab'
beforeEach(() => {
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      disconnect() {}
    },
  )
})
afterEach(() => {
  cleanup()
  vi.useRealTimers()
  vi.unstubAllGlobals()
  vi.clearAllMocks()
  useLab.setState({ active: undefined, focus: undefined })
})
it('mounts both session panels, includes the live viewer and throttles pose control', async () => {
  vi.useFakeTimers()
  const scene: Scene = {
    scenario: 'arm',
    params: {},
    timestep: 0.02,
    bodies: [],
    geoms: [],
    cameras: [],
    actuators: [],
    camera_default: { pos: [1, 1, 1], target: [0, 0, 0] },
  }
  mocks.scenario = {
    id: 'arm',
    description: { en: 'Arm' },
    title: { en: 'Arm' },
    inputs: ['pose'],
    has_vision: true,
    reward_terms: [],
  } as unknown as ScenarioInfo
  mocks.call.mockResolvedValue({ mode: 'pose' })
  useLab.setState({ active: { session: 'ses', scene } })
  render(<RobotLab />)
  expect(screen.queryByText('Arm', { selector: 'p' })).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: 'controls' }))
  expect(screen.getByRole('dialog', { name: 'controls' })).toHaveAttribute('aria-modal', 'false')
  expect(document.querySelector('.lab-sheet-backdrop')).toBeNull()
  fireEvent.keyDown(screen.getByRole('dialog'), { key: 'Escape' })
  expect(screen.queryByRole('dialog')).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: 'chat' }))
  expect(screen.getByText('Chat excalibur')).toBeVisible()
  fireEvent.click(screen.getByRole('button', { name: 'controls' }))
  expect(screen.getByRole('button', { name: 'controls' })).toHaveAttribute('aria-expanded', 'true')
  expect(screen.getByRole('dialog', { name: 'controls' })).toBeVisible()
  fireEvent.click(screen.getByRole('button', { name: 'chat' }))
  expect(screen.queryByRole('dialog')).toBeNull()
  expect(screen.getByRole('button', { name: 'controls' })).toHaveAttribute('aria-expanded', 'false')
  fireEvent.click(screen.getByRole('tab', { name: 'vision' }))
  expect(screen.getByText('Vision ses')).toBeVisible()
  fireEvent.click(screen.getByRole('tab', { name: 'teleop' }))
  expect(screen.getByText('Viewer ses')).toBeVisible()
  fireEvent.click(screen.getByRole('button', { name: 'Send pose' }))
  await act(() => vi.advanceTimersByTime(1))
  fireEvent.click(screen.getByRole('button', { name: 'Send pose' }))
  expect(mocks.call).toHaveBeenCalledTimes(1)
  await act(() => vi.advanceTimersByTime(34))
  expect(mocks.call).toHaveBeenCalledTimes(2)
  expect(mocks.call).toHaveBeenLastCalledWith('robotlab.session.control', {
    session: 'ses',
    mode: 'pose',
    pose: { landmarks: [], t: 1 },
  })
  await act(() => vi.advanceTimersByTime(1))
  fireEvent.click(screen.getByRole('button', { name: 'Send pose' }))
  await act(async () => {
    useLab.setState({ focus: { session: 'ses', view: 'vision' }, sequence: 1 })
  })
  expect(screen.getByRole('tab', { name: 'vision' })).toHaveAttribute('aria-selected', 'true')
  fireEvent.click(screen.getByRole('tab', { name: 'teleop' }))
  expect(screen.getByRole('tab', { name: 'teleop' })).toHaveAttribute('aria-selected', 'true')
  await act(async () => {
    useLab.setState({ focus: { session: 'ses', view: 'vision' }, sequence: 2 })
  })
  expect(screen.getByRole('tab', { name: 'vision' })).toHaveAttribute('aria-selected', 'true')
  cleanup()
  await act(() => vi.advanceTimersByTime(100))
  expect(mocks.call).toHaveBeenCalledTimes(2)
})
