import { render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { expect, it, vi } from 'vitest'
const call = vi.hoisted(() => vi.fn())
vi.mock('./api', () => ({ robotlabEnabled: false, labCall: call, useRobotEvent: vi.fn() }))
vi.mock('./i18n', () => ({ useLabText: () => ({ text: (key: string) => key, language: 'en' }) }))
import { ToolStore } from './ToolStore'
it('keeps the disabled store informative without offering installation or querying the daemon', () => {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={client}>
      <ToolStore />
    </QueryClientProvider>,
  )
  expect(screen.getByText('unavailable')).toBeVisible()
  expect(screen.queryByRole('button', { name: 'install' })).not.toBeInTheDocument()
  expect(screen.getAllByRole('img')).toHaveLength(6)
  expect(call).not.toHaveBeenCalled()
  client.clear()
})
