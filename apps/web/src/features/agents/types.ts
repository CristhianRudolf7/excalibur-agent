export type { AvatarSpec } from '@/components/avatar-spec'
import type { AvatarSpec } from '@/components/avatar-spec'

export interface Agent {
  id: string
  name: string
  title: string
  description: string
  personality?: string
  avatar: AvatarSpec
  favorite: boolean
  archived: boolean
  conversation_id: string
  created_at: string
  updated_at: string
  last_message_at: string | null
  unread: number
  last_preview: string
}
export interface AgentList {
  agents: Agent[]
  favorite_id: string
}
export interface AgentFields {
  name: string
  title?: string
  description?: string
  personality?: string
  avatar?: AvatarSpec
}
export interface AgentMethods {
  'agents.list': { params: { include_archived?: boolean }; result: AgentList }
  'agents.get': { params: { id: string }; result: Agent }
  'agents.create': { params: AgentFields; result: Agent }
  'agents.update': { params: { id: string } & Partial<AgentFields>; result: Agent }
  'agents.archive': { params: { id: string }; result: { favorite_id: string } }
  'agents.restore': { params: { id: string }; result: Agent }
  'agents.set_favorite': { params: { id: string }; result: { favorite_id: string } }
  'agents.mark_read': { params: { id: string }; result: { unread: 0 } }
}
export const agentEventTypes = [
  'agents.created',
  'agents.updated',
  'agents.archived',
  'agents.restored',
  'agents.favorite_changed',
  'agents.unread',
]
export interface AgentUnread {
  agent_id: string
  unread: number
  last_preview: string
  last_message_at: string | null
}

export interface AgentEvents {
  'agents.created': Agent
  'agents.updated': Agent
  'agents.archived': Agent
  'agents.restored': Agent
  'agents.favorite_changed': { favorite_id: string }
  'agents.unread': AgentUnread
}
