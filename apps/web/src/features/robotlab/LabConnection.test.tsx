import { beforeEach, expect, it, vi } from 'vitest'
import { cleanup, render } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { LabConnection } from './LabConnection'
import { useLab } from './state'
const mocks = vi.hoisted(() => ({
  navigate: vi.fn(),
  events: new Map<string, (payload: unknown) => void>(),
}))
vi.mock('@tanstack/react-router', () => ({ useNavigate: () => mocks.navigate }))
vi.mock('./api', () => ({
  useRobotEvent: (name: string, callback: (payload: unknown) => void) => {
    mocks.events.set(name, callback)
  },
}))
beforeEach(() => {
  cleanup()
  vi.clearAllMocks()
  mocks.events.clear()
  useLab.setState({ agentOpen: false })
})
it('opens a focused session from any route with the shared agent column', () => {
  const client = new QueryClient()
  const invalidate = vi.spyOn(client, 'invalidateQueries')
  render(
    <QueryClientProvider client={client}>
      <LabConnection />
    </QueryClientProvider>,
  )
  mocks.events.get('robotlab.focus')?.({
    session: 'training-1',
    robot: 'arm',
    environment: 'table',
  })
  expect(mocks.navigate).toHaveBeenCalledWith({
    to: '/lab',
    search: { tab: 'trainings', session: 'training-1' },
  })
  expect(useLab.getState().agentOpen).toBe(true)
  expect(invalidate).toHaveBeenCalledWith({ queryKey: ['robotlab'] })
})
it('refreshes session headers, lists and skills on session events', () => {
  const client = new QueryClient()
  const invalidate = vi.spyOn(client, 'invalidateQueries')
  render(
    <QueryClientProvider client={client}>
      <LabConnection />
    </QueryClientProvider>,
  )
  const session = { id: 'training-1', status: 'stopped' }
  mocks.events.get('robotlab.session')?.({ session })
  expect(client.getQueryData(['robotlab', 'session', 'training-1'])).toEqual({ session })
  expect(invalidate).toHaveBeenCalledWith({ queryKey: ['robotlab', 'sessions'] })
  expect(invalidate).toHaveBeenCalledWith({ queryKey: ['robotlab', 'skills', 'training-1'] })
})
