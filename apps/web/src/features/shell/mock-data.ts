export type AgentId = string
export type Color =
  'violet' | 'blue' | 'green' | 'yellow' | 'orange' | 'red' | 'pink' | 'gray' | 'primary'
export interface Agent {
  id: AgentId
  name: string
  role: string
  color: Color
  unread?: number
  working?: boolean
}
const agents: Agent[] = [{ id: 'excalibur', name: 'Excalibur', role: 'master', color: 'primary' }]
export const getAgents = () => agents
export const getAgent = (_id: string) => agents[0]!
