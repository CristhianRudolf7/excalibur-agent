import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import i18n from '@/lib/i18n'
import { rpc } from '@/lib/rpc/client'
import { Artifacts } from './Artifacts'
import { receiveArtifactEvent, useArtifacts } from './store'
import type { Artifact } from './types'
const overview: Artifact = {
  id: 'overview',
  title: 'System overview',
  kind: 'diagram',
  format: 'svg',
  mime: 'image/svg+xml',
  size: 50,
  created_at: '2026-11-02T10:00:00Z',
  group: 'system',
}
const detail = {
  ...overview,
  id: 'detail',
  title: 'Storage detail',
  created_at: '2026-11-02T10:01:00Z',
}
beforeEach(() => {
  void i18n.changeLanguage('en')
  useArtifacts.setState({ autoOpen: false, conversations: {} })
  Object.defineProperty(URL, 'createObjectURL', {
    configurable: true,
    value: vi.fn(() => 'blob:diagram'),
  })
  Object.defineProperty(URL, 'revokeObjectURL', { configurable: true, value: vi.fn() })
  vi.spyOn(rpc, 'call').mockImplementation(async (_method, params) => ({
    id: (params as { id: string }).id,
    mime: 'image/svg+xml',
    content: '<svg xmlns="http://www.w3.org/2000/svg"><rect width="20" height="20"/></svg>',
    source: 'graph TD; A-->B',
    dialect: 'Mermaid',
  }))
})
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  useArtifacts.setState({ conversations: {} })
})
const renderView = () =>
  render(
    <QueryClientProvider
      client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
    >
      <Artifacts session="active" />
    </QueryClientProvider>,
  )
it('starts with a grouped overview and navigates details while listing the conversation newest first', async () => {
  receiveArtifactEvent(
    {
      type: 'artifact.ready',
      payload: { session_id: 'active', turn_id: 'one', artifacts: [overview, detail] },
    },
    'active',
  )
  renderView()
  expect(screen.getAllByRole('option').map((node) => node.textContent)).toEqual([
    'Storage detailSVG',
    'System overviewSVG',
  ])
  expect(await screen.findByRole('img', { name: 'System overview' })).toHaveAttribute(
    'src',
    'blob:diagram',
  )
  fireEvent.click(screen.getByRole('button', { name: 'Storage detail' }))
  expect(await screen.findByRole('img', { name: 'Storage detail' })).toBeVisible()
  expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:diagram')
  fireEvent.keyDown(screen.getByRole('listbox'), { key: 'ArrowDown' })
  expect(await screen.findByRole('img', { name: 'System overview' })).toBeVisible()
})
it('can leave the just-finished turn and revisit persisted conversation artifacts', async () => {
  receiveArtifactEvent(
    {
      type: 'artifact.ready',
      payload: { session_id: 'active', turn_id: 'old', artifacts: [overview] },
    },
    'active',
  )
  useArtifacts.getState().setAutoOpen(true)
  receiveArtifactEvent(
    {
      type: 'artifact.ready',
      payload: { session_id: 'active', turn_id: 'new', artifacts: [detail] },
    },
    'active',
    () => {},
  )
  await useArtifacts.persist.rehydrate()
  renderView()
  expect(screen.getAllByRole('option')).toHaveLength(1)
  fireEvent.click(screen.getByRole('button', { name: 'View all diagrams' }))
  expect(screen.getAllByRole('option')).toHaveLength(2)
  fireEvent.click(screen.getByRole('option', { name: /System overview/ }))
  expect(await screen.findByRole('img', { name: 'System overview' })).toBeVisible()
})
it('reports RPC failures and retries the selected artifact', async () => {
  vi.mocked(rpc.call).mockRejectedValueOnce(new Error('Unavailable'))
  receiveArtifactEvent(
    {
      type: 'artifact.ready',
      payload: { session_id: 'active', turn_id: 'one', artifacts: [overview] },
    },
    'active',
  )
  renderView()
  expect(await screen.findByRole('alert')).toHaveTextContent('Could not load the diagram.')
  fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
  await waitFor(() => expect(screen.getByRole('img', { name: 'System overview' })).toBeVisible())
})
