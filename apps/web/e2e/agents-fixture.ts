import type { Page } from '@playwright/test'
import type { Agent, AgentList } from '../src/features/agents/types'
import type { ConversationMessage } from '../src/lib/rpc/contract'
import { defaultAgent } from '../src/features/agents/store'
import { mockEvents } from './event-fixture'

export async function mockAgents(page: Page) {
  const bus = await mockEvents(page)
  let agents: Agent[] = [
    {
      ...defaultAgent,
      id: 'sam',
      name: 'Sam',
      favorite: false,
      conversation_id: 'cnv_agent_sam',
      unread: 2,
      last_preview: 'Sam private history',
      last_message_at: '2026-11-03T12:00:00Z',
    },
    {
      ...defaultAgent,
      id: 'ada',
      name: 'Ada',
      favorite: true,
      last_preview: 'Ada private history',
    },
    {
      ...defaultAgent,
      id: 'old',
      name: 'Archived',
      favorite: false,
      archived: true,
      conversation_id: 'cnv_agent_old',
    },
  ]
  let favorite = 'ada'
  const requests: { method: string; params?: Record<string, unknown> }[] = []
  const histories: Record<string, ConversationMessage[]> = {
    cnv_web_excalibur: [
      {
        id: 'ada-private',
        conversation_id: 'cnv_web_excalibur',
        role: 'assistant',
        text: 'Ada private history',
      },
    ],
    cnv_agent_sam: [
      {
        id: 'sam-private',
        conversation_id: 'cnv_agent_sam',
        role: 'user',
        text: 'Sam private history',
        relayed_by: 'ada',
      },
    ],
  }
  await page.route('**/rpc', async (route) => {
    const request = route.request().postDataJSON()
    requests.push(request)
    let result: unknown
    const id = request.params?.id
    switch (request.method) {
      case 'credentials.pending':
        result = { requests: [] }
        break
      case 'agents.list':
        result = { agents, favorite_id: favorite } satisfies AgentList
        break
      case 'agents.mark_read':
        agents = agents.map((agent) => (agent.id === id ? { ...agent, unread: 0 } : agent))
        result = { unread: 0 }
        break
      case 'agents.set_favorite':
        favorite = id
        agents = agents.map((agent) => ({ ...agent, favorite: agent.id === favorite }))
        result = { favorite_id: favorite }
        break
      case 'agents.archive':
        agents = agents.map((agent) => (agent.id === id ? { ...agent, archived: true } : agent))
        if (favorite === id) favorite = agents.find((agent) => !agent.archived)!.id
        agents = agents.map((agent) => ({ ...agent, favorite: agent.id === favorite }))
        result = { favorite_id: favorite }
        break
      case 'agents.restore':
        agents = agents.map((agent) => (agent.id === id ? { ...agent, archived: false } : agent))
        result = agents.find((agent) => agent.id === id)
        break
      case 'agents.update':
        agents = agents.map((agent) => (agent.id === id ? { ...agent, ...request.params } : agent))
        result = agents.find((agent) => agent.id === id)
        break
      case 'conversation.history':
        result = { messages: histories[request.params.conversation_id] ?? [] }
        break
      case 'conversation.send': {
        const message = {
          id: `sent-${requests.length}`,
          role: 'user',
          text: request.params.text,
          conversation_id: request.params.conversation_id,
        }
        ;(histories[message.conversation_id] ??= []).push(message)
        bus.emit('message.created', { ...message, message_id: message.id })
        result = { msg_id: message.id }
        break
      }
      default:
        return route.fallback()
    }
    return route.fulfill({ json: { jsonrpc: '2.0', id: request.id, result } })
  })
  return {
    requests,
    bus,
    unread(id: string, unread: number, preview: string) {
      const at = new Date().toISOString()
      agents = agents.map((agent) =>
        agent.id === id ? { ...agent, unread, last_preview: preview, last_message_at: at } : agent,
      )
      bus.emit('agents.unread', {
        agent_id: id,
        unread,
        last_preview: preview,
        last_message_at: at,
      })
    },
  }
}
