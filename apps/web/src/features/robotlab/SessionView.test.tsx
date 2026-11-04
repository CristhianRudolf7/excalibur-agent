import { beforeEach, expect, it, vi } from 'vitest'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import i18n from '@/lib/i18n'
import { SessionView } from './SessionView'
import { Activity, Skills } from './SessionAgent'
import type { Session } from './types'
import { useLab } from './state'
const mocks = vi.hoisted(() => ({ call: vi.fn(), live: vi.fn(), replay: vi.fn(), image: vi.fn() }))
vi.mock('./api', () => ({ labCall: mocks.call, sessionImage: mocks.image, useRobotEvent: vi.fn() }))
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
  max_attempts: 20,
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
  expect(await screen.findByRole('heading', { name: 'Dog with arm · Test room' })).toBeVisible()
  expect(screen.getByText('Chat: excalibur')).toBeVisible()
  expect(mocks.live).toHaveBeenCalledWith('session-1')
  fireEvent.click(screen.getByRole('button', { name: 'Swap views' }))
  expect(screen.getByRole('img', { name: 'Robot camera' })).toHaveClass('lab-observer')
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
  expect(await screen.findByRole('heading', { name: 'Dog with arm · room' })).toBeVisible()
  await waitFor(() => expect(mocks.call).toHaveBeenCalledWith('robotlab.env.get', { id: 'room' }))
})
it('selects an old attempt and uses replay rather than live frames', async () => {
  mocks.call.mockResolvedValue({ session: { ...session, status: 'stopped' } })
  mount(<SessionView id="session-1" mobile={false} onBack={vi.fn()} />)
  await screen.findByLabelText('Attempt')
  fireEvent.change(screen.getByLabelText('Attempt'), { target: { value: '1' } })
  expect(mocks.replay).toHaveBeenLastCalledWith('session-1', 1)
  expect(mocks.live).toHaveBeenLastCalledWith(undefined)
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
it('shows versioned skill success, use count and code availability', async () => {
  mocks.call.mockResolvedValue({
    skills: [
      {
        name: 'fetch',
        description: 'Fetch ball',
        version: 3,
        success_rate: 0.75,
        uses: 4,
        has_code: true,
      },
    ],
  })
  mount(<Skills session={session.id} />)
  expect(await screen.findByText('fetch')).toBeVisible()
  expect(screen.getByText(/Version: 3.*Success: 75%.*Uses: 4/)).toBeVisible()
  expect(screen.getByText('Has code')).toBeVisible()
})

it('opens the mobile agent tab when a session focus arrives', async () => {
  mount(<SessionView id="session-1" mobile onBack={vi.fn()} />)
  await screen.findByRole('img', { name: 'Observer' })
  act(() => useLab.getState().focusSession('session-1'))
  expect(screen.getByText('Chat: excalibur')).toBeVisible()
  expect(screen.getByRole('tab', { name: 'Agent' })).toHaveAttribute('aria-selected', 'true')
})
