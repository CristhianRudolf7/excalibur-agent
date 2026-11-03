import { useEffect } from 'react'
import { useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query'
import { rpc } from '@/lib/rpc/client'
import { subscribeEvents } from '@/lib/rpc/events'
import { useAgents } from './store'
import { agentEventTypes, type AgentList, type AgentUnread } from './types'

export const agentsKey = ['personal-agents'] as const
export function applyAgentList(client: QueryClient, list: AgentList) {
  useAgents.getState().apply(list)
  client.setQueryData(agentsKey, list)
}
export async function refreshAgents(client: QueryClient) {
  const list = await rpc.call('agents.list', { include_archived: true })
  applyAgentList(client, list)
}
export async function markAgentRead(id: string) {
  // Preserve an unread event received while the acknowledgement was in flight.
  const agent = useAgents.getState().agents.find((item) => item.id === id)
  await rpc.call('agents.mark_read', { id })
  const current = useAgents.getState().agents.find((item) => item.id === id)
  if (current === agent && current)
    useAgents.getState().unread({
      agent_id: id,
      unread: 0,
      last_preview: current.last_preview,
      last_message_at: current.last_message_at,
    })
}
/** The shell owns one list query and one resumable subscription for every interface. */
export function useAgentsQuery() {
  return useQuery({
    queryKey: agentsKey,
    queryFn: async ({ signal }) => {
      const list = await rpc.call('agents.list', { include_archived: true }, { signal })
      useAgents.getState().apply(list)
      return list
    },
    retry: false,
    staleTime: 30_000,
  })
}
export function useAgentsConnection() {
  const client = useQueryClient()
  const query = useAgentsQuery()
  useEffect(() => {
    if (query.isError) useAgents.setState({ loaded: true })
  }, [query.isError])
  useEffect(
    () =>
      subscribeEvents({
        types: agentEventTypes,
        event: (event) => {
          if (event.type === 'agents.unread') {
            const update = event.payload as AgentUnread
            useAgents.getState().unread(update)
            client.setQueryData<AgentList>(
              agentsKey,
              (list) =>
                list && {
                  ...list,
                  agents: list.agents.map((agent) =>
                    agent.id === update.agent_id ? { ...agent, ...update } : agent,
                  ),
                },
            )
          } else if (agentEventTypes.includes(event.type)) {
            void client.invalidateQueries({ queryKey: agentsKey })
          }
        },
        ready: () => {
          void client.invalidateQueries({ queryKey: agentsKey })
        },
        offline: () => {},
      }),
    [client],
  )
  return query
}
