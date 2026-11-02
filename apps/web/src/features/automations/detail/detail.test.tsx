import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { rpc, RpcError } from '@/lib/rpc/client'
import i18n from '@/lib/i18n'
import { FlowDiagram } from './FlowDiagram'
import { AutomationDetailPage } from './AutomationDetailPage'
import { sampleAutomation, sampleRun, sampleVersions } from './fixtures'
beforeEach(async () => {
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      disconnect() {}
    },
  )
  await i18n.changeLanguage('en')
})
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})
function mount() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  })
  client.setQueryData(['automation', sampleAutomation.id], {
    automation: sampleAutomation,
    versions: sampleVersions,
  })
  client.setQueryData(['automation-runs', sampleAutomation.id], { runs: [sampleRun] })
  render(
    <QueryClientProvider client={client}>
      <AutomationDetailPage id={sampleAutomation.id} actions={() => <button>Edit</button>} />
    </QueryClientProvider>,
  )
  return client
}
it('renders every real step type, language, agent avatar, and schedule node', () => {
  render(
    <FlowDiagram automation={sampleAutomation} schedule="Daily at 09:00" onSelect={() => {}} />,
  )
  expect(
    screen.getAllByRole('button', {
      name: /^(Agent|Script|Tool|Condition|Notification|Schedule):/,
    }),
  ).toHaveLength(6)
  expect(screen.getByText('Python')).toBeVisible()
  expect(document.querySelector('.auto-type-agent .pixel-avatar')).toBeTruthy()
  expect(screen.getByText('Yes')).toBeVisible()
  expect(screen.getByText('No')).toBeVisible()
})
it('applies all replay classes and errors', () => {
  render(
    <FlowDiagram
      automation={sampleAutomation}
      schedule="Daily"
      run={{
        ...sampleRun,
        steps: [
          { step_id: 's1', status: 'succeeded' },
          { step_id: 's2', status: 'running' },
          { step_id: 's3', status: 'failed', error: 'Script failed' },
          { step_id: 's4', status: 'skipped' },
        ],
      }}
      onSelect={() => {}}
    />,
  )
  for (const [id, status] of [
    ['s1', 'succeeded'],
    ['s2', 'running'],
    ['s3', 'failed'],
    ['s4', 'skipped'],
    ['s5', 'pending'],
  ])
    expect(document.querySelector(`[data-step-id="${id}"]`)).toHaveClass(`auto-step-${status}`)
  expect(document.querySelector('[data-step-id="s3"]')).toHaveAttribute('title', 'Script failed')
})
it('supports arrows and Enter selection', async () => {
  const select = vi.fn()
  render(<FlowDiagram automation={sampleAutomation} schedule="Daily" onSelect={select} />)
  const trigger = screen.getByRole('button', { name: 'Schedule: Schedule' })
  trigger.focus()
  await userEvent.keyboard('{ArrowDown}{Enter}')
  expect(screen.getByRole('button', { name: /Agent: Review/ })).toHaveFocus()
  expect(select).toHaveBeenLastCalledWith('s1')
})
it('selects details and replays a run that progresses through cache events', async () => {
  const client = mount()
  fireEvent.click(screen.getByRole('button', { name: /Agent: Review/ }))
  expect(
    screen.getByText(sampleAutomation.steps[0]!.instruction!, { selector: '.auto-detail-field p' }),
  ).toBeVisible()
  fireEvent.click(screen.getByRole('tab', { name: 'Runs' }))
  fireEvent.click(screen.getByRole('button', { name: /Scheduled/ }))
  expect(document.querySelector('[data-step-id="s1"]')).toHaveClass('auto-step-succeeded')
  act(() =>
    client.setQueryData(['automation-runs', sampleAutomation.id], {
      runs: [{ ...sampleRun, status: 'running', steps: [{ step_id: 's1', status: 'running' }] }],
    }),
  )
  await waitFor(() =>
    expect(document.querySelector('[data-step-id="s1"]')).toHaveClass('auto-step-running'),
  )
  expect(screen.getByText('Live')).toBeVisible()
  act(() =>
    client.setQueryData(['automation-runs', sampleAutomation.id], {
      runs: [
        {
          ...sampleRun,
          status: 'failed',
          steps: [{ step_id: 's1', status: 'failed', error: 'Run failed' }],
        },
      ],
    }),
  )
  await waitFor(() =>
    expect(document.querySelector('[data-step-id="s1"]')).toHaveClass('auto-step-failed'),
  )
  fireEvent.click(screen.getByRole('tab', { name: 'Versions' }))
  expect(screen.getByText('First approved version.')).toBeVisible()
})
it('avoids applying old-version statuses to the current graph', () => {
  render(
    <FlowDiagram
      automation={sampleAutomation}
      schedule="Daily"
      run={{ ...sampleRun, version: 1 }}
      onSelect={() => {}}
    />,
  )
  expect(document.querySelector('.auto-step-succeeded')).toBeNull()
})
it('renders a layout skeleton without loading text', () => {
  vi.spyOn(rpc, 'call').mockImplementation(() => new Promise(() => {}))
  render(
    <QueryClientProvider client={new QueryClient()}>
      <AutomationDetailPage id="missing" />
    </QueryClientProvider>,
  )
  expect(document.querySelector('[aria-busy="true"]')).toBeTruthy()
  expect(screen.queryByText(/loading/i)).toBeNull()
})
it('renders not-found with a back link', async () => {
  vi.spyOn(rpc, 'call').mockRejectedValue(new RpcError(-32000, 'automation not found'))
  render(
    <QueryClientProvider client={new QueryClient()}>
      <AutomationDetailPage id="missing" />
    </QueryClientProvider>,
  )
  expect(await screen.findByText('Automation not found.')).toBeVisible()
  expect(screen.getByRole('link')).toHaveAttribute('href', '/automations')
})
it('retries a failed detail request', async () => {
  let online = false
  const call = vi.spyOn(rpc, 'call').mockImplementation(async (method) => {
    if (!online) throw new Error('offline')
    return (
      method === 'automations.get'
        ? { automation: sampleAutomation, versions: sampleVersions }
        : { runs: [] }
    ) as never
  })
  render(
    <QueryClientProvider client={new QueryClient()}>
      <AutomationDetailPage id="missing" />
    </QueryClientProvider>,
  )
  expect(await screen.findByText('Could not load automation.')).toBeVisible()
  online = true
  fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
  expect(await screen.findByRole('heading', { name: sampleAutomation.name })).toBeVisible()
  expect(call).toHaveBeenCalledTimes(3)
})

it('uses the newest finished run when a live run completes without manual selection', async () => {
  const client = mount()
  act(() =>
    client.setQueryData(['automation-runs', sampleAutomation.id], {
      runs: [{ ...sampleRun, status: 'running', steps: [{ step_id: 's1', status: 'running' }] }],
    }),
  )
  await waitFor(() =>
    expect(document.querySelector('[data-step-id="s1"]')).toHaveClass('auto-step-running'),
  )
  act(() => client.setQueryData(['automation-runs', sampleAutomation.id], { runs: [sampleRun] }))
  await waitFor(() =>
    expect(document.querySelector('[data-step-id="s1"]')).toHaveClass('auto-step-succeeded'),
  )
})
