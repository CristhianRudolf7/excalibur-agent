import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { cleanup, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import i18n from '@/lib/i18n'
import { rpc } from '@/lib/rpc/client'
import { useArtifacts } from '@/features/artifacts/store'
import Settings from './Settings'

const route = vi.hoisted(() => ({ section: 'general', navigate: vi.fn() }))
vi.mock('@tanstack/react-router', () => ({
  useSearch: () => ({ section: route.section }),
  useNavigate: () => route.navigate,
}))
vi.mock('@/features/agents/AgentSettings', () => ({ AgentSettings: () => null }))
const clients: QueryClient[] = []
beforeEach(async () => {
  route.section = 'general'
  route.navigate.mockClear()
  document.documentElement.classList.remove('dark')
  useArtifacts.setState({ autoOpen: true })
  await i18n.changeLanguage('en')
  vi.spyOn(rpc, 'call').mockResolvedValue({
    id: 'local',
    name: 'Ana Example',
    timezone: 'UTC',
    timezone_source: 'manual',
    language: 'en',
  })
})
afterEach(() => {
  cleanup()
  clients.forEach((client) => client.clear())
  clients.length = 0
  vi.restoreAllMocks()
})
function setup() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  clients.push(client)
  return render(
    <QueryClientProvider client={client}>
      <Settings />
    </QueryClientProvider>,
  )
}
it('exposes the artifact preference as a labeled keyboard switch and persists it', async () => {
  const user = userEvent.setup()
  setup()
  const toggle = screen.getByRole('switch', {
    name: 'Automatically open diagrams when each turn finishes',
  })
  expect(toggle).toBeChecked()
  expect(toggle).toHaveAccessibleDescription('Show generated diagrams alongside your conversation.')
  toggle.focus()
  await user.keyboard(' ')
  expect(toggle).not.toBeChecked()
  expect(useArtifacts.getState().autoOpen).toBe(false)
  expect(JSON.parse(localStorage.getItem('excalibur-artifacts')!).state.autoOpen).toBe(false)
  await user.keyboard('{Enter}')
  expect(toggle).toBeChecked()
})
it('navigates all sections with keyboard shortcuts and activates only on Enter', async () => {
  const user = userEvent.setup()
  setup()
  const nav = screen.getByRole('navigation', { name: 'Settings' })
  const general = within(nav).getByRole('button', { name: 'General' })
  general.focus()
  await user.keyboard('{ArrowDown}')
  expect(within(nav).getByRole('button', { name: 'Profile' })).toHaveFocus()
  expect(route.navigate).not.toHaveBeenCalled()
  await user.keyboard('{End}')
  expect(within(nav).getByRole('button', { name: 'Passwords' })).toHaveFocus()
  await user.keyboard('{ArrowRight}')
  expect(general).toHaveFocus()
  await user.keyboard('{ArrowLeft}{Enter}')
  expect(route.navigate).toHaveBeenCalledWith({ to: '/settings', search: { section: 'passwords' } })
  expect(general).toHaveAttribute('aria-current', 'page')
})
it('retains segmented language and theme behavior with pressed state and persistence', async () => {
  const user = userEvent.setup()
  setup()
  const appearance = screen.getByRole('group', { name: 'Appearance' })
  await user.click(within(appearance).getByRole('button', { name: 'Dark' }))
  expect(document.documentElement).toHaveClass('dark')
  expect(localStorage.getItem('excalibur-ui-theme')).toBe('dark')
  expect(within(appearance).getByRole('button', { name: 'Dark' })).toHaveAttribute(
    'aria-pressed',
    'true',
  )
  await user.click(screen.getByRole('button', { name: 'Español' }))
  expect(screen.getByRole('heading', { name: 'Configuración' })).toBeVisible()
  expect(
    screen.getByRole('switch', { name: 'Abrir diagramas automáticamente al terminar cada turno' }),
  ).toBeChecked()
})

it('shows a named skeleton while the read-only profile is loading', async () => {
  route.section = 'profile'
  vi.mocked(rpc.call).mockImplementation(() => new Promise(() => {}))
  setup()
  expect(await screen.findByRole('status', { name: 'Loading…' })).toBeVisible()
  expect(screen.queryByRole('textbox')).not.toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Profile' })).toHaveAttribute('aria-current', 'page')
})
