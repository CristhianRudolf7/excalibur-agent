import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import i18n from '@/lib/i18n'
import { rpc } from '@/lib/rpc/client'
import { AgentSwitcher, AgentChatLink } from './AgentSwitcher'
import { defaultAgent, useAgents } from './store'

const ada = { ...defaultAgent, id: 'ada', name: 'Ada', favorite: true }
const sam = { ...defaultAgent, id: 'sam', name: 'Sam', favorite: false }
beforeEach(async () => {
  await i18n.changeLanguage('en')
  useAgents.setState({
    agents: [sam, { ...sam, id: 'old', name: 'Old', archived: true }, ada],
    favorite_id: ada.id,
    selectedId: undefined,
  })
  vi.spyOn(rpc, 'call').mockResolvedValue({ unread: 0 })
})
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})
it('defaults to the favorite and lists only active agents with the favorite first', async () => {
  const user = userEvent.setup()
  render(<AgentSwitcher />)
  await user.click(screen.getByRole('button', { name: 'Switch agent: Ada' }))
  expect(
    screen.getAllByRole('menuitemradio').map((node) => node.getAttribute('aria-label')),
  ).toEqual(['Ada', 'Sam'])
  expect(screen.getByRole('menuitemradio', { name: 'Ada' })).toHaveAttribute('aria-checked', 'true')
})
it('switches every shared header and its chat target, marks read and calls the route handler', async () => {
  const user = userEvent.setup()
  const change = vi.fn()
  render(
    <>
      <AgentSwitcher onChange={change} />
      <AgentSwitcher />
      <AgentChatLink />
    </>,
  )
  await user.click(screen.getAllByRole('button', { name: 'Switch agent: Ada' })[0]!)
  await user.click(screen.getByRole('menuitemradio', { name: 'Sam' }))
  expect(change).toHaveBeenCalledWith('sam')
  expect(screen.getAllByRole('button', { name: 'Switch agent: Sam' })).toHaveLength(2)
  expect(screen.getByRole('link', { name: 'Open conversation' })).toHaveAttribute(
    'href',
    '/agents/sam?view=chat',
  )
  expect(rpc.call).toHaveBeenCalledWith('agents.mark_read', { id: 'sam' })
})
it('falls back to the favorite when the selected agent is archived', () => {
  useAgents.getState().select('sam')
  useAgents.getState().apply({ agents: [ada, { ...sam, archived: true }], favorite_id: 'ada' })
  render(<AgentSwitcher />)
  expect(screen.getByRole('button', { name: 'Switch agent: Ada' })).toBeInTheDocument()
})
it('keeps names literal and localizes the switcher label', async () => {
  await i18n.changeLanguage('es')
  render(<AgentSwitcher />)
  expect(screen.getByRole('button', { name: 'Cambiar agente: Ada' })).toBeInTheDocument()
})
