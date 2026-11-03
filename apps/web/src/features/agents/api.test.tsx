import { act, cleanup, render, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { rpc } from '@/lib/rpc/client'
import { FakeSocket } from '@/features/chat/fake-socket'
import { agentsKey, useAgentsConnection } from './api'
import { defaultAgent, selectedAgent, useAgents } from './store'
import { agentEventTypes, type AgentList } from './types'

const ada = { ...defaultAgent, id: 'ada', name: 'Ada' }
const sam = { ...defaultAgent, id: 'sam', name: 'Sam', favorite: false }
function Connection() {
  useAgentsConnection()
  return null
}
beforeEach(() => {
  FakeSocket.peers = []
  vi.stubGlobal('WebSocket', FakeSocket)
  useAgents.setState({
    agents: [defaultAgent],
    favorite_id: defaultAgent.id,
    selectedId: undefined,
    loaded: false,
  })
})
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})
it('subscribes to agent events, updates unread state and refreshes identities after lifecycle events', async () => {
  let list: AgentList = { agents: [ada, sam], favorite_id: ada.id }
  vi.spyOn(rpc, 'call').mockImplementation(async () => list as never)
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={client}>
      <Connection />
    </QueryClientProvider>,
  )
  await waitFor(() => expect(useAgents.getState().loaded).toBe(true))
  const socket = FakeSocket.peers[0]!
  act(() => {
    socket.onopen?.()
    socket.ack()
  })
  expect(JSON.parse(socket.send.mock.calls[0]![0]).params.types).toEqual(agentEventTypes)
  const unread = {
    agent_id: sam.id,
    unread: 4,
    last_preview: 'New reply',
    last_message_at: '2026-11-03T12:00:00Z',
  }
  act(() => {
    socket.frame({
      jsonrpc: '2.0',
      method: 'event',
      params: { id: 'unread', seq: 1, type: 'agents.unread', payload: unread },
    })
  })
  expect(useAgents.getState().agents.find((agent) => agent.id === sam.id)).toMatchObject(unread)
  expect(client.getQueryData<AgentList>(agentsKey)?.agents[1]).toMatchObject(unread)
  act(() => useAgents.getState().select(sam.id))
  list = { agents: [ada, { ...sam, archived: true }], favorite_id: ada.id }
  act(() => {
    socket.frame({
      jsonrpc: '2.0',
      method: 'event',
      params: { id: 'archive', seq: 2, type: 'agents.archived', payload: { agent_id: sam.id } },
    })
  })
  await waitFor(() => expect(useAgents.getState().selectedId).toBeUndefined())
  expect(selectedAgent(useAgents.getState()).id).toBe(ada.id)
  expect(rpc.call).toHaveBeenCalledWith(
    'agents.list',
    { include_archived: true },
    expect.objectContaining({ signal: expect.any(AbortSignal) }),
  )
  client.clear()
})
it('finishes initial loading on RPC failure so the legacy conversation stays reachable', async () => {
  vi.spyOn(rpc, 'call').mockRejectedValue(new Error('Daemon unavailable'))
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={client}>
      <Connection />
    </QueryClientProvider>,
  )
  await waitFor(() => expect(useAgents.getState().loaded).toBe(true))
  expect(selectedAgent(useAgents.getState()).conversation_id).toBe('cnv_web_excalibur')
  expect(client.getQueryState(agentsKey)?.status).toBe('error')
  client.clear()
})
