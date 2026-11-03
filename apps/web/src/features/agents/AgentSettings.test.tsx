import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import i18n from '@/lib/i18n'
import { rpc } from '@/lib/rpc/client'
import { defaultAgent, useAgents } from './store'
import { AgentSettings } from './AgentSettings'
import type { AgentList } from './types'

const navigate = vi.hoisted(() => vi.fn())
vi.mock('@tanstack/react-router', () => ({ useNavigate: () => navigate }))
let list: AgentList
const ada = { ...defaultAgent, id: 'ada', name: 'Ada', favorite: true }
const sam = { ...defaultAgent, id: 'sam', name: 'Sam', favorite: false }
function mount() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <AgentSettings />
    </QueryClientProvider>,
  )
}
beforeEach(async () => {
  await i18n.changeLanguage('en')
  list = {
    agents: [ada, sam, { ...sam, id: 'old', name: 'Archived', archived: true }],
    favorite_id: ada.id,
  }
  useAgents.setState({
    agents: [defaultAgent],
    favorite_id: defaultAgent.id,
    selectedId: undefined,
    drafts: {},
  })
  vi.spyOn(rpc, 'call').mockImplementation(async (method, params) => {
    if (method === 'agents.list') return list as never
    const id = (params as { id: string }).id
    if (method === 'agents.set_favorite') {
      list = { ...list, favorite_id: id }
      return { favorite_id: id } as never
    }
    if (method === 'agents.archive') {
      list = {
        agents: list.agents.map((agent) =>
          agent.id === id ? { ...agent, archived: true } : agent,
        ),
        favorite_id: 'sam',
      }
      return { favorite_id: 'sam' } as never
    }
    if (method === 'agents.restore') {
      const restored = { ...list.agents.find((agent) => agent.id === id)!, archived: false }
      list = { ...list, agents: list.agents.map((agent) => (agent.id === id ? restored : agent)) }
      return restored as never
    }
    if (method === 'agents.update') {
      const updated = { ...list.agents.find((agent) => agent.id === id)!, ...params }
      list = { ...list, agents: list.agents.map((agent) => (agent.id === id ? updated : agent)) }
      return updated as never
    }
    throw new Error(`Unexpected method: ${method}`)
  })
  HTMLDialogElement.prototype.showModal = function () {
    this.setAttribute('open', '')
  }
  HTMLDialogElement.prototype.close = function () {
    this.removeAttribute('open')
  }
})
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  navigate.mockReset()
})
async function card(name: string) {
  return within(await screen.findByRole('article', { name }))
}
it('changes the favorite using the confirmed server ID and restores archived agents', async () => {
  mount()
  fireEvent.click((await card('Sam')).getByRole('button', { name: 'Choose as favorite' }))
  await waitFor(() => expect(useAgents.getState().favorite_id).toBe('sam'))
  expect((await card('Sam')).getByText('Favorite')).toBeInTheDocument()
  fireEvent.click((await card('Archived')).getByRole('button', { name: 'Restore' }))
  expect(await screen.findByText('No archived agents.')).toBeInTheDocument()
  expect((await card('Archived')).getByRole('button', { name: 'Edit' })).toBeInTheDocument()
})
it('archives only after confirmation, explains favorite hand-over and accepts the new favorite', async () => {
  mount()
  fireEvent.click((await card('Ada')).getByRole('button', { name: 'Archive' }))
  const dialog = screen.getByRole('dialog', { name: 'Archive Ada' })
  expect(within(dialog).getByText(/most recently used/)).toBeInTheDocument()
  expect(rpc.call).not.toHaveBeenCalledWith('agents.archive', expect.anything())
  fireEvent.click(within(dialog).getByRole('button', { name: 'Archive' }))
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
  expect(rpc.call).toHaveBeenCalledWith('agents.archive', { id: 'ada' })
  expect(useAgents.getState().favorite_id).toBe('sam')
  expect((await card('Ada')).getByRole('button', { name: 'Restore' })).toBeInTheDocument()
})
it('prevents archiving the last active agent', async () => {
  list = { agents: [ada], favorite_id: ada.id }
  mount()
  expect((await card('Ada')).getByRole('button', { name: 'Archive' })).toBeDisabled()
  expect(screen.getByText('The last active agent cannot be archived.')).toBeInTheDocument()
})
it('saves all editable fields and retains the form after an RPC failure', async () => {
  mount()
  fireEvent.click((await card('Sam')).getByRole('button', { name: 'Edit' }))
  const dialog = screen.getByRole('dialog', { name: 'Edit' })
  for (const [label, value] of [
    ['Agent name', 'Samuel'],
    ['Title', 'Engineer'],
    ['Description', 'Builds tools'],
    ['Personality and instructions', 'Be precise'],
  ] as const)
    fireEvent.change(within(dialog).getByLabelText(label), { target: { value } })
  vi.mocked(rpc.call).mockRejectedValueOnce(new Error('Unavailable'))
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
  expect(await within(dialog).findByRole('alert')).toHaveTextContent('Could not save')
  expect(within(dialog).getByLabelText('Agent name')).toHaveValue('Samuel')
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
  expect(rpc.call).toHaveBeenCalledWith('agents.update', {
    id: 'sam',
    name: 'Samuel',
    title: 'Engineer',
    description: 'Builds tools',
    personality: 'Be precise',
  })
  expect(await screen.findByRole('article', { name: 'Samuel' })).toBeInTheDocument()
})
it.each(['en', 'es'])(
  'opens the favorite chat with an unsent avatar request in %s',
  async (language) => {
    await i18n.changeLanguage(language)
    mount()
    fireEvent.click((await card('Sam')).getByRole('button', { name: i18n.t('agentRegenerate') }))
    const dialog = screen.getByRole('dialog', { name: i18n.t('agentRegenerate') })
    fireEvent.change(within(dialog).getByRole('textbox'), { target: { value: 'blue headphones' } })
    fireEvent.click(within(dialog).getByRole('button', { name: i18n.t('agentAvatarOpen') }))
    expect(useAgents.getState().drafts.ada?.text).toBe(
      i18n.t('agentAvatarRequest', { name: 'Sam', description: 'blue headphones' }),
    )
    expect(navigate).toHaveBeenCalledWith(expect.objectContaining({ params: { agentId: 'ada' } }))
    expect(vi.mocked(rpc.call).mock.calls.some(([method]) => method === 'conversation.send')).toBe(
      false,
    )
  },
)
