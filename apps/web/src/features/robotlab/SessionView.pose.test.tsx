import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import i18n from '@/lib/i18n'
import type { Session } from './types'
import type { Frame } from './viewer/types'
import { frameFixture, sceneFixture } from './viewer/fixtures.test-support'

const mocks = vi.hoisted(() => ({ call: vi.fn(), receive: (_frame: Frame) => {} }))
vi.mock('./api', () => ({ labCall: mocks.call }))
vi.mock('./viewer/rpc', () => ({
  viewerCall: mocks.call,
  subscribeFrames: (receive: (frame: Frame) => void) => {
    mocks.receive = receive
    return () => {}
  },
}))
vi.mock('./viewer', async () => ({
  ...(await import('./viewer/hooks')),
  SceneView: ({ frame, label }: { frame: Frame | null; label: string }) => (
    <div role="img" aria-label={label} data-pose={frame ? `${frame.attempt}:${frame.seq}` : ''} />
  ),
  ReplayControls: () => null,
}))
vi.mock('./SessionAgent', () => ({ SessionAgent: () => null }))
import { SessionView } from './SessionView'

const session: Session = {
  id: 'session',
  name: 'Training',
  robot: 'dog',
  environment: 'room',
  task: 'Climb the stairs',
  goal: { type: 'robot_on', target: { object: 'landing' } },
  status: 'live',
  attempt: 2,
  max_attempts: 40,
  created_at: '2026-01-01',
  ended_at: null,
  conversation: null,
  skill_events: [],
  outcome: null,
  attempts: [1, 2].map((n) => ({
    n,
    status: n === 1 ? 'failed' : 'running',
    reason: '',
    started_at: '2026-01-01',
    ended_at: n === 1 ? '2026-01-01' : null,
    sim_time: 0,
    frames: 0,
    skills_used: [],
  })),
}
let client: QueryClient
const animations: FrameRequestCallback[] = []
beforeEach(async () => {
  await i18n.changeLanguage('en')
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  animations.length = 0
  vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) =>
    animations.push(callback),
  )
  vi.stubGlobal('cancelAnimationFrame', () => {})
  mocks.call.mockReset().mockImplementation(async (method: string) => {
    if (method === 'robotlab.session.get') return { session }
    if (method === 'robotlab.env.get') return { environment: { name: 'Room' } }
    if (method === 'robotlab.session.scene') return { scene: sceneFixture }
    throw new Error(`Unexpected call: ${method}`)
  })
})
afterEach(() => {
  cleanup()
  client.clear()
  vi.unstubAllGlobals()
})
function mount() {
  return render(
    <QueryClientProvider client={client}>
      <SessionView id="session" mobile onBack={() => {}} />
    </QueryClientProvider>,
  )
}
function recordFrames(reply: (attempt: number, from: number) => unknown) {
  const original = mocks.call.getMockImplementation()!
  mocks.call.mockImplementation((method: string, params: { attempt: number; from: number }) =>
    method === 'robotlab.attempt.frames'
      ? reply(params.attempt, params.from)
      : original(method, params),
  )
}
function emit(frame: Frame) {
  act(() => {
    mocks.receive(frame)
    animations.splice(0).forEach((callback) => callback(16))
  })
}
it('applies the last recorded pose on mount without any live frame and then accepts live updates', async () => {
  recordFrames((_attempt, from) =>
    Promise.resolve({
      frames: [frameFixture(from, { attempt: 2 })],
      total: 10,
    }),
  )
  mount()
  const observer = await screen.findByRole('img', { name: 'Observer' })
  await waitFor(() => expect(observer).toHaveAttribute('data-pose', '2:9'))
  emit(frameFixture(10, { attempt: 2 }))
  expect(observer).toHaveAttribute('data-pose', '2:10')
})
it('does not overwrite a newer live pose when the older initial fetch completes later', async () => {
  let finish: (value: { frames: Frame[]; total: number }) => void = () => {}
  recordFrames(
    () =>
      new Promise((resolve) => {
        finish = resolve
      }),
  )
  mount()
  const observer = await screen.findByRole('img', { name: 'Observer' })
  await waitFor(() =>
    expect(mocks.call.mock.calls.some(([method]) => method === 'robotlab.attempt.frames')).toBe(
      true,
    ),
  )
  emit(frameFixture(20, { attempt: 2 }))
  expect(observer).toHaveAttribute('data-pose', '2:20')
  await act(async () => finish({ frames: [frameFixture(1, { attempt: 2 })], total: 1 }))
  expect(observer).toHaveAttribute('data-pose', '2:20')
})
it('falls back to the previous attempt until the current attempt emits a live pose', async () => {
  recordFrames((attempt) =>
    Promise.resolve(
      attempt === 2 ? { frames: [], total: 0 } : { frames: [frameFixture(19)], total: 1 },
    ),
  )
  mount()
  const observer = await screen.findByRole('img', { name: 'Observer' })
  await waitFor(() => expect(observer).toHaveAttribute('data-pose', '1:19'))
  emit(frameFixture(0, { attempt: 2 }))
  expect(observer).toHaveAttribute('data-pose', '2:0')
})
it('opens stopped sessions at the last pose and falls back if their last attempt is empty', async () => {
  const stopped = { ...session, status: 'stopped' }
  client.setQueryData(['robotlab', 'session', session.id], { session: stopped })
  const original = mocks.call.getMockImplementation()!
  mocks.call.mockImplementation((method: string, params: unknown) =>
    method === 'robotlab.session.get'
      ? Promise.resolve({ session: stopped })
      : original(method, params),
  )
  recordFrames((attempt) =>
    Promise.resolve(
      attempt === 2 ? { frames: [], total: 0 } : { frames: [frameFixture(12)], total: 1 },
    ),
  )
  mount()
  const observer = await screen.findByRole('img', { name: 'Observer' })
  await waitFor(() => expect(observer).toHaveAttribute('data-pose', '1:12'))
})
