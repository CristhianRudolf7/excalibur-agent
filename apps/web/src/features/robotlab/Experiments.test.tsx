import '@/lib/i18n'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, expect, it, vi } from 'vitest'
import type { Experiment, ScenarioInfo } from './types'
const mocks = vi.hoisted(() => ({ call: vi.fn() }))
vi.mock('./api', () => ({ labCall: mocks.call, useRobotEvent: vi.fn() }))
vi.mock('./RewardChart', () => ({ RewardChart: () => null }))
vi.mock('./Viewer', () => ({ Viewer: () => null }))
vi.mock('./CreateExperiment', () => ({ CreateExperiment: () => null }))
import { Experiments } from './Experiments'
import { useLab } from './state'
import { useSceneRuns } from './sceneRun'
afterEach(() => {
  cleanup()
  vi.resetAllMocks()
  useSceneRuns.setState({ playback: undefined })
  useLab.setState({ active: undefined })
})
it('shows one training action, pending transitions, progress and playback stop', async () => {
  let experiment: Experiment = {
    id: 'e',
    name: 'Balance',
    scenario: 'cartpole',
    params: {},
    status: 'draft',
    created_at: '',
    total_steps: 100,
    reward_weights: {},
    code: '',
    runs: [
      {
        id: 'r',
        label: 'PPO',
        algorithm: 'PPO',
        hyperparams: {},
        status: 'draft',
        steps: 25,
        wall_s: 0,
        checkpoints: ['best'],
      },
    ],
  }
  let resolve: (value: Experiment) => void = () => {}
  mocks.call.mockImplementation(async (method: string) => {
    if (method.endsWith('.list')) return { experiments: [experiment] }
    if (method.endsWith('.get')) return experiment
    if (method.endsWith('.metrics')) return { points: [], next: 0 }
    if (method === 'robotlab.session.open') return { session: 'preview', scene: {} }
    if (method.endsWith('.start') || method.endsWith('.stop'))
      return new Promise<Experiment>((done) => {
        resolve = done
      })
    return { mode: 'idle' }
  })
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={client}>
      <Experiments
        scenario={{} as ScenarioInfo}
        weights={{}}
        selected="e"
        onSelect={vi.fn()}
        onPlay={async () => {
          useSceneRuns.setState({ playback: 'r' })
        }}
      />
    </QueryClientProvider>,
  )
  const start = await screen.findByRole('button', { name: /Start training|Iniciar entrenamiento/ })
  expect(
    screen.queryByRole('button', { name: /Stop training|Detener entrenamiento/ }),
  ).not.toBeInTheDocument()
  fireEvent.click(start)
  expect(screen.getByRole('button', { name: /Starting|Iniciando/ })).toBeDisabled()
  experiment = { ...experiment, status: 'running' }
  await act(async () => resolve(experiment))
  const stop = await screen.findByRole('button', { name: /Stop training|Detener entrenamiento/ })
  expect(screen.getAllByRole('status').some((node) => node.textContent?.includes('25 / 100'))).toBe(
    true,
  )
  fireEvent.click(stop)
  expect(screen.getByRole('button', { name: /Stopping|Deteniendo/ })).toBeDisabled()
  experiment = { ...experiment, status: 'stopped' }
  await act(async () => resolve(experiment))
  expect(
    await screen.findByRole('button', { name: /Start training|Iniciar entrenamiento/ }),
  ).toBeEnabled()
  fireEvent.click(
    screen.getByRole('button', { name: /Play best policy|Reproducir mejor política/ }),
  )
  const playing = await screen.findByRole('button', { name: /Playing|Reproduciendo/ })
  await waitFor(() => expect(playing).toBeEnabled())
  fireEvent.click(playing)
  await waitFor(() =>
    expect(mocks.call).toHaveBeenCalledWith('robotlab.session.control', {
      session: '',
      mode: 'idle',
    }),
  )
  client.clear()
})
