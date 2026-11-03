import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import i18n from '@/lib/i18n'
import { AgentSidebar } from './AgentSidebar'
import { defaultAgent, useAgents, orderAgents } from './store'
import { Composer } from '@/features/chat/Composer'

beforeEach(async () => {
  await i18n.changeLanguage('en')
  useAgents.setState({ drafts: {} })
})
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})
const favorite = { ...defaultAgent, id: 'favorite', name: 'Ada', last_preview: 'Ready to help' }
const recent = {
  ...defaultAgent,
  id: 'recent',
  name: 'Sam',
  favorite: false,
  last_message_at: '2026-11-03T12:00:00Z',
  unread: 3,
  last_preview: 'Your draft is ready',
}
it('orders active agents with the favorite first, then recent chats without mutating input', () => {
  const agents = [
    recent,
    {
      ...recent,
      id: 'older',
      unread: 0,
      last_preview: 'Older answer',
      last_message_at: '2026-11-02T12:00:00Z',
    },
    favorite,
    { ...favorite, id: 'archived', archived: true },
  ]
  const select = vi.fn()
  render(<AgentSidebar agents={agents} selectedId="recent" onSelect={select} onCreate={() => {}} />)
  const rows = within(screen.getByRole('navigation', { name: 'Agents' })).getAllByRole('button')
  expect(rows.map((row) => row.getAttribute('aria-label'))).toEqual(['Ada', 'Sam', 'Sam'])
  expect(orderAgents(agents).map((agent) => agent.id)).toEqual(['favorite', 'recent', 'older'])
  expect(agents[0]!.id).toBe('recent')
  expect(screen.getByLabelText('Favorite')).toBeInTheDocument()
  expect(screen.getByLabelText('3 unread')).toBeInTheDocument()
  expect(screen.getByText('Your draft is ready')).toBeInTheDocument()
  expect(document.querySelectorAll('time')).toHaveLength(2)
  fireEvent.click(rows[1]!)
  expect(select).toHaveBeenCalledWith('recent')
})
it('updates unread badges and previews after a live unread event', () => {
  useAgents.setState({ agents: [recent] })
  function LiveSidebar() {
    const agents = useAgents((state) => state.agents)
    return <AgentSidebar agents={agents} onSelect={() => {}} onCreate={() => {}} />
  }
  render(<LiveSidebar />)
  fireEvent.click(screen.getByRole('button', { name: 'Create agent' }))
  // Use a DOM event to batch the store update in React's event boundary.
  const update = () =>
    useAgents.getState().unread({
      agent_id: recent.id,
      unread: 8,
      last_preview: 'New answer',
      last_message_at: '2026-11-03T12:01:00Z',
    })
  render(<button onClick={update}>Update</button>)
  fireEvent.click(screen.getByRole('button', { name: 'Update' }))
  expect(screen.getByLabelText('8 unread')).toBeInTheDocument()
  expect(screen.getByText('New answer')).toBeInTheDocument()
})
it.each(['en', 'es'])(
  'prefills and focuses the favorite composer without sending in %s',
  async (language) => {
    await i18n.changeLanguage(language)
    const send = vi.fn()
    const create = () => useAgents.getState().prefill('favorite', i18n.t('agentCreateRequest'))
    render(
      <>
        <AgentSidebar agents={[favorite]} onSelect={() => {}} onCreate={create} />
        <Composer agent="Ada" agentId="favorite" typing={false} onSend={send} onStop={() => {}} />
      </>,
    )
    fireEvent.click(screen.getByRole('button', { name: i18n.t('agentCreate') }))
    expect(screen.getByRole('textbox')).toHaveValue(
      language === 'en'
        ? 'Hi, can you create an additional agent, please?'
        : 'Hola, ¿puedes crear un agente adicional, por favor?',
    )
    expect(screen.getByRole('textbox')).toHaveFocus()
    expect(send).not.toHaveBeenCalled()
  },
)
it('does not consume a prefill addressed to another chat', () => {
  useAgents.getState().prefill('favorite', 'Create an agent')
  render(
    <Composer agent="Sam" agentId="recent" typing={false} onSend={() => {}} onStop={() => {}} />,
  )
  expect(screen.getByRole('textbox')).toHaveValue('')
  expect(useAgents.getState().drafts.favorite?.text).toBe('Create an agent')
})
it('disables selection and creation while the registry is loading', () => {
  const select = vi.fn(),
    create = vi.fn()
  render(<AgentSidebar agents={[favorite]} disabled onSelect={select} onCreate={create} />)
  const row = screen.getByRole('button', { name: 'Ada' })
  const button = screen.getByRole('button', { name: 'Create agent' })
  expect(row).toBeDisabled()
  expect(button).toBeDisabled()
  fireEvent.click(row)
  fireEvent.click(button)
  expect(select).not.toHaveBeenCalled()
  expect(create).not.toHaveBeenCalled()
})
