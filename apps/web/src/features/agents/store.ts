import { create } from 'zustand'
import { paladinAvatar } from '@/components/avatar-spec'
import { agentAvatar } from './avatar'
import type { Agent, AgentList, AgentUnread } from './types'

// Keep the existing installation reachable while the agent list is loading.
export const defaultAgent: Agent = {
  id: 'excalibur',
  name: 'Excalibur',
  title: '',
  description: '',
  avatar: paladinAvatar,
  favorite: true,
  archived: false,
  conversation_id: 'cnv_web_excalibur',
  created_at: '',
  updated_at: '',
  last_message_at: '',
  unread: 0,
  last_preview: '',
}
export function orderAgents(agents: readonly Agent[]) {
  return agents
    .filter((agent) => !agent.archived)
    .sort(
      (a, b) =>
        Number(b.favorite) - Number(a.favorite) ||
        (b.last_message_at ?? '').localeCompare(a.last_message_at ?? '') ||
        a.created_at.localeCompare(b.created_at) ||
        a.id.localeCompare(b.id),
    )
}
interface AgentsState extends AgentList {
  selectedId?: string
  loaded: boolean
  drafts: Record<string, { text: string; request: number } | undefined>
  apply: (list: AgentList) => void
  select: (id: string) => void
  unread: (update: AgentUnread) => void
  prefill: (id: string, text: string) => void
  clearPrefill: (id: string, request: number) => void
}
export const useAgents = create<AgentsState>((set) => ({
  agents: [defaultAgent],
  favorite_id: defaultAgent.id,
  loaded: false,
  drafts: {},
  apply: (list) =>
    set((state) => ({
      ...list,
      agents: list.agents.map((agent) => ({
        ...agent,
        avatar: agentAvatar(agent),
        favorite: agent.id === list.favorite_id,
      })),
      loaded: true,
      selectedId: list.agents.some((agent) => agent.id === state.selectedId && !agent.archived)
        ? state.selectedId
        : undefined,
    })),
  select: (selectedId) => set({ selectedId }),
  unread: (update) =>
    set((state) => ({
      agents: state.agents.map((agent) =>
        agent.id === update.agent_id ? { ...agent, ...update } : agent,
      ),
    })),
  prefill: (id, text) =>
    set((state) => ({
      drafts: { ...state.drafts, [id]: { text, request: (state.drafts[id]?.request ?? 0) + 1 } },
    })),
  clearPrefill: (id, request) =>
    set((state) =>
      state.drafts[id]?.request === request ? { drafts: { ...state.drafts, [id]: undefined } } : {},
    ),
}))
export const selectedAgent = (state: Pick<AgentsState, 'agents' | 'selectedId' | 'favorite_id'>) =>
  state.agents.find(
    (agent) => agent.id === (state.selectedId ?? state.favorite_id) && !agent.archived,
  ) ??
  state.agents.find((agent) => agent.id === state.favorite_id && !agent.archived) ??
  state.agents.find((agent) => !agent.archived) ??
  defaultAgent
export function useSelectedAgent() {
  return useAgents(selectedAgent)
}
export function useAgent(id: string) {
  return useAgents((state) => state.agents.find((agent) => agent.id === id) ?? defaultAgent)
}
