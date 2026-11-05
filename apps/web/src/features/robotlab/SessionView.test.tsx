import { beforeEach, expect, it, vi } from 'vitest'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import i18n from '@/lib/i18n'
import { SessionView } from './SessionView'
import { LabConnection } from './LabConnection'
import { Activity, Skills, SessionAgent } from './SessionAgent'
import type { Session } from './types'
import { useLab } from './state'
const mocks = vi.hoisted(() => ({
  call: vi.fn(),
  live: vi.fn(),
  replay: vi.fn(),
  image: vi.fn(),
  navigate: vi.fn(),
  events: new Map<string, (payload: unknown) => void>(),
}))
vi.mock('@tanstack/react-router', () => ({ useNavigate: () => mocks.navigate }))
vi.mock('./api', () => ({
  labCall: mocks.call,
  sessionImage: mocks.image,
  useRobotEvent: (name: string, callback: (payload: unknown) => void) => {
    mocks.events.set(name, callback)
  },
}))
vi.mock('@/features/chat/Chat', () => ({
  Chat: ({ agentId }: { agentId: string }) => <div>Chat: {agentId}</div>,
}))
vi.mock('./viewer', () => ({
  SceneView: ({ label, view, className }: { label: string; view: string; className?: string }) => (
    <div role="img" aria-label={label} data-view={view} className={className} />
  ),
  useScene: () => ({ scene: null, loading: false }),
  useLiveFrame: mocks.live,
  useLatestRecordedFrame: () => null,
  useReplay: mocks.replay,
  ReplayControls: ({ t }: { t: number }) => <div>Replay: {t}</div>,
}))
const session: Session = {
  id: 'session-1',
  name: 'Training',
  robot: 'dog',
  environment: 'room',
  task: 'Fetch ball',
  goal: { type: 'object_held', object: 'ball' },
  status: 'live',
  created_at: '2026-01-01',
  ended_at: null,
  max_attempts: 40,
  attempt: 2,
  attempts: [
    {
      n: 1,
      status: 'failed',
      reason: 'fell',
      started_at: '2026-01-01',
      ended_at: '2026-01-01',
      sim_time: 5,
      frames: 100,
      skills_used: [],
    },
    {
      n: 2,
      status: 'running',
      reason: '',
      started_at: '2026-01-01',
      ended_at: null,
      sim_time: 1,
      frames: 20,
      skills_used: [],
    },
  ],
  conversation: null,
  skill_events: [],
  outcome: null,
}
function mount(node: React.ReactNode) {
  return render(
    <QueryClientProvider
      client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
    >
      {node}
    </QueryClientProvider>,
  )
}
beforeEach(async () => {
  cleanup()
  vi.clearAllMocks()
  mocks.events.clear()
  mocks.image.mockResolvedValue({ png: 'observation-png' })
  await i18n.changeLanguage('en')
  useLab.setState({ agentOpen: false, agentRequest: 0, focusedSession: undefined })
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      disconnect() {}
    },
  )
  mocks.live.mockReturnValue(null)
  mocks.replay.mockReturnValue({
    frame: null,
    t: 0,
    duration: 10,
    playing: false,
    loading: false,
    play: vi.fn(),
    pause: vi.fn(),
    seek: vi.fn(),
    speed: 1,
    setSpeed: vi.fn(),
  })
  mocks.call.mockImplementation(async (method: string) => {
    if (method === 'robotlab.env.get') return { environment: { id: 'room', name: 'Test room' } }
    if (method === 'robotlab.session.get') return { session }
    if (method === 'robotlab.session.stop') return { session: { ...session, status: 'stopped' } }
    if (method === 'robotlab.session.log') return { entries: [], next: 0 }
    if (method === 'robotlab.skills.list') return { skills: [] }
    return {}
  })
})
it('renders observer, inset camera, goal and shared chat for a live session', async () => {
  // A desktop-sized split leaves both scene views visible beside the agent.
  vi.stubGlobal(
    'ResizeObserver',
    class {
      callback: ResizeObserverCallback
      constructor(callback: ResizeObserverCallback) {
        this.callback = callback
      }
      observe(target: Element) {
        this.callback(
          [{ target, contentRect: { width: 1400 } } as ResizeObserverEntry],
          this as unknown as ResizeObserver,
        )
      }
      disconnect() {}
    },
  )
  mount(<SessionView id="session-1" mobile={false} onBack={vi.fn()} />)
  expect(await screen.findByRole('img', { name: 'Observer' })).toBeVisible()
  expect(screen.getByRole('img', { name: 'Robot camera' })).toBeVisible()
  expect(screen.getByText('Goal: Hold ball')).toBeVisible()
  expect(await screen.findByRole('heading', { name: 'Dog · Test room' })).toBeVisible()
  expect(screen.getByText('Chat: excalibur')).toBeVisible()
  expect(mocks.live).toHaveBeenCalledWith('session-1', 2)
  fireEvent.click(screen.getByRole('button', { name: 'Swap views' }))
  expect(
    screen.getByRole('img', { name: 'Robot camera' }).closest('.lab-robot-camera'),
  ).toHaveClass('lab-observer')
  fireEvent.click(screen.getByRole('button', { name: 'Stop session' }))
  await waitFor(() =>
    expect(mocks.call).toHaveBeenCalledWith('robotlab.session.stop', {
      session: 'session-1',
      by: 'user',
    }),
  )
  await waitFor(() =>
    expect(screen.queryByRole('button', { name: 'Stop session' })).not.toBeInTheDocument(),
  )
})
it('falls back to the environment id when its details are unavailable', async () => {
  const original = mocks.call.getMockImplementation()!
  mocks.call.mockImplementation((method: string, ...args: unknown[]) => {
    if (method === 'robotlab.env.get') return Promise.reject(new Error('not found'))
    return original(method, ...args)
  })
  mount(<SessionView id="session-1" mobile onBack={vi.fn()} />)
  expect(await screen.findByRole('heading', { name: 'Dog · room' })).toBeVisible()
  await waitFor(() => expect(mocks.call).toHaveBeenCalledWith('robotlab.env.get', { id: 'room' }))
})
it('selects an old attempt and uses replay rather than live frames', async () => {
  mocks.call.mockResolvedValue({ session: { ...session, status: 'stopped' } })
  mount(<SessionView id="session-1" mobile={false} onBack={vi.fn()} />)
  await screen.findByLabelText('Attempt')
  fireEvent.change(screen.getByLabelText('Attempt'), { target: { value: '1' } })
  expect(mocks.replay).toHaveBeenLastCalledWith('session-1', 1, true)
  expect(mocks.live).toHaveBeenLastCalledWith(undefined, undefined)
  expect(screen.getByText('Replay: 0')).toBeInTheDocument()
})
it('uses observer/camera/agent navigation on mobile', async () => {
  mount(<SessionView id="session-1" mobile onBack={vi.fn()} />)
  await screen.findByRole('img', { name: 'Observer' })
  fireEvent.click(screen.getByRole('tab', { name: 'Robot camera' }))
  expect(screen.getByRole('img', { name: 'Robot camera' })).toBeVisible()
  expect(screen.queryByRole('img', { name: 'Observer' })).not.toBeInTheDocument()
  fireEvent.click(screen.getByRole('tab', { name: 'Agent' }))
  expect(screen.getByText('Chat: excalibur')).toBeVisible()
})
it('syncs recorded log entries to replay time and includes look thumbnails/reflections', async () => {
  mocks.call.mockResolvedValue({
    entries: [
      {
        id: 'look',
        t: '2026-01-01',
        session: session.id,
        attempt: 1,
        kind: 'look',
        summary: 'Visible ball',
        image: 'attempts/1/looks/1.png',
        sim_time: 1,
      },
      {
        id: 'reflection',
        t: '2026-01-01',
        session: session.id,
        attempt: 1,
        kind: 'reflection',
        summary: 'Approach more slowly',
        sim_time: 4,
      },
    ],
    next: 2,
  })
  const view = mount(<Activity session={{ ...session, status: 'stopped' }} attempt={1} time={2} />)
  expect(await screen.findByText('Visible ball')).toBeVisible()
  expect(await screen.findByRole('img', { name: 'Observation' })).toHaveAttribute(
    'src',
    'data:image/png;base64,observation-png',
  )
  expect(screen.queryByText('Approach more slowly')).not.toBeInTheDocument()
  view.unmount()
  mount(<Activity session={{ ...session, status: 'stopped' }} attempt={1} time={5} />)
  expect(await screen.findByText('Approach more slowly')).toBeVisible()
})
it('shows markdown skill instructions, versions and failure accounting', async () => {
  mocks.call.mockImplementation(async (method: string) =>
    method === 'robotlab.skills.read'
      ? {
          skill: {
            instructions:
              '# Lessons\n\nKeep three feet planted.\n\n```json\n{"keyframes": []}\n```',
            versions: [
              { version: 1, created_at: '2026-01-01' },
              { version: 3, created_at: '2026-01-03' },
            ],
          },
        }
      : {
          skills: [
            {
              name: 'fetch',
              description: 'Fetch ball',
              version: 3,
              success_rate: 0.75,
              uses: 4,
              failures: 2,
            },
          ],
        },
  )
  mount(<Skills session={session.id} />)
  expect(await screen.findByText('fetch')).toBeVisible()
  expect(screen.getByText(/Version: 3.*Success: 75%.*Uses: 4/)).toBeVisible()
  expect(screen.getByText(/Failures: 2/)).toBeVisible()
  fireEvent.click(screen.getByText('fetch'))
  // Wait for the detail response and the lazy Markdown module before checking
  // rendered headings; cold transforms can exceed the query polling timeout.
  await screen.findByText(/Keep three feet planted/)
  await act(() => vi.dynamicImportSettled())
  expect(await screen.findByRole('heading', { name: 'Lessons' })).toBeVisible()
  expect(screen.getByRole('list', { name: 'Versions' })).toHaveTextContent('Version 1')
  expect(screen.getByRole('list', { name: 'Versions' })).toHaveTextContent('Version 3')
  expect(mocks.call).toHaveBeenCalledWith('robotlab.skills.read', {
    session: session.id,
    name: 'fetch',
  })
})

it('opens the mobile agent tab when a session focus arrives', async () => {
  mount(<SessionView id="session-1" mobile onBack={vi.fn()} />)
  await screen.findByRole('img', { name: 'Observer' })
  act(() => useLab.getState().focusSession('session-1'))
  expect(screen.getByText('Chat: excalibur')).toBeVisible()
  expect(screen.getByRole('tab', { name: 'Agent' })).toHaveAttribute('aria-selected', 'true')
})

it('shows logged dog state beside chat and activity without changing the selected view', async () => {
  const original = mocks.call.getMockImplementation()!
  mocks.call.mockImplementation((method: string, ...args: unknown[]) => {
    if (method === 'robotlab.session.log')
      return Promise.resolve({
        entries: [
          {
            id: 'state',
            t: '2026-01-01',
            session: session.id,
            attempt: 2,
            kind: 'act',
            tool: 'sequence',
            summary: 'Moved trunk',
            sim_time: 1,
            result: {
              state: {
                support: ['FL', 'FR', 'RL'],
                margin: 0.025,
                z: 0.27,
                pitch_deg: 1,
                roll_deg: 0,
              },
            },
          },
        ],
        next: 1,
      })
    return original(method, ...args)
  })
  mount(<SessionView id="session-1" mobile onBack={vi.fn()} />)
  await screen.findByRole('img', { name: 'Observer' })
  fireEvent.click(screen.getByRole('tab', { name: 'Agent' }))
  expect(await screen.findByText('FL · FR · RL')).toBeVisible()
  expect(screen.getByText('Chat: excalibur')).toBeVisible()
  fireEvent.click(screen.getByRole('tab', { name: 'Activity' }))
  expect(screen.getByText('FL · FR · RL')).toBeVisible()
  expect(await screen.findByText('Moved trunk')).toBeVisible()
})

it('uses read-only state results for live sessions whose older logs have no state', async () => {
  const original = mocks.call.getMockImplementation()!
  mocks.call.mockImplementation((method: string, ...args: unknown[]) => {
    if (method === 'robotlab.robot.state')
      return Promise.resolve({
        state: { gripper: { pos: [0.1, 0.2, 0.9], opening: 0.5 } },
        held: 'cube',
      })
    return original(method, ...args)
  })
  mount(<SessionAgent session={{ ...session, robot: 'arm' }} attempt={2} />)
  expect(await screen.findByText('cube')).toBeVisible()
  expect(screen.getByText('0.100 / 0.200 / 0.900 m')).toBeVisible()
  expect(mocks.call).toHaveBeenCalledWith(
    'robotlab.robot.state',
    { session: session.id },
    expect.anything(),
  )
  expect(mocks.call.mock.calls.some(([method]) => method === 'robotlab.robot.act')).toBe(false)
})
it('keeps historical state empty when no state was recorded rather than reading the live world', async () => {
  mount(<SessionAgent session={{ ...session, status: 'stopped' }} attempt={1} time={0} />)
  expect(screen.getByText('No robot state recorded yet.')).toBeVisible()
  await waitFor(() =>
    expect(mocks.call).toHaveBeenCalledWith(
      'robotlab.session.log',
      { session: session.id, attempt: 1 },
      expect.anything(),
    ),
  )
  expect(mocks.call.mock.calls.some(([method]) => method === 'robotlab.robot.state')).toBe(false)
})

it('focus switches a resumed session from an old replay to the new live attempt', async () => {
  const stopped = { ...session, status: 'stopped' as const }
  const original = mocks.call.getMockImplementation()!
  mocks.call.mockImplementation((method: string, ...args: unknown[]) =>
    method === 'robotlab.session.get'
      ? Promise.resolve({ session: stopped })
      : original(method, ...args),
  )
  mount(
    <>
      <LabConnection />
      <SessionView id="session-1" mobile onBack={vi.fn()} />
    </>,
  )
  await screen.findByLabelText('Attempt')
  fireEvent.change(screen.getByLabelText('Attempt'), { target: { value: '1' } })
  expect(mocks.replay).toHaveBeenLastCalledWith('session-1', 1, true)
  const resumed: Session = {
    ...session,
    attempt: 3,
    max_attempts: 42,
    attempts: [...session.attempts, { ...session.attempts[1]!, n: 3, frames: 0, sim_time: 0 }],
  }
  mocks.call.mockImplementation((method: string, ...args: unknown[]) =>
    method === 'robotlab.session.get'
      ? Promise.resolve({ session: resumed })
      : original(method, ...args),
  )
  act(() => {
    mocks.events.get('robotlab.session')?.({ session: resumed })
    mocks.events.get('robotlab.focus')?.({ session: session.id, robot: 'dog', environment: 'room' })
  })
  await waitFor(() => expect(screen.getByLabelText('Attempt')).toHaveValue('3'))
  expect(screen.getByLabelText('Attempt')).toBeDisabled()
  expect(screen.getAllByRole('option').map((option) => option.getAttribute('value'))).toEqual([
    '1',
    '2',
    '3',
  ])
  expect(screen.queryByText('Replay: 0')).not.toBeInTheDocument()
  expect(mocks.replay).toHaveBeenLastCalledWith(undefined, undefined, true)
  expect(mocks.live).toHaveBeenLastCalledWith('session-1', 3)
  expect(screen.getByText('Chat: excalibur')).toBeVisible()
  expect(screen.getByRole('button', { name: 'Stop session' })).toBeVisible()
  expect(mocks.navigate).toHaveBeenCalledWith({
    to: '/lab',
    search: { tab: 'trainings', session: session.id },
  })
  act(() => mocks.events.get('robotlab.session')?.({ session: { ...resumed, status: 'stopped' } }))
  await waitFor(() => expect(screen.getByLabelText('Attempt')).toBeEnabled())
  expect(screen.getByLabelText('Attempt')).toHaveValue('3')
  fireEvent.change(screen.getByLabelText('Attempt'), { target: { value: '1' } })
  expect(mocks.replay).toHaveBeenLastCalledWith('session-1', 1, true)
})
