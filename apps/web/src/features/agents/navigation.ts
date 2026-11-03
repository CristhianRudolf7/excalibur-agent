import { useNavigate } from '@tanstack/react-router'
import { useAgents } from './store'

/** Prefills are addressed to an identity, so opening another chat cannot consume them. */
export function useOpenAgentChat() {
  const navigate = useNavigate()
  return (text?: string, id = useAgents.getState().favorite_id) => {
    useAgents.getState().select(id)
    if (text !== undefined) useAgents.getState().prefill(id, text)
    void navigate({
      to: '/agents/$agentId',
      params: { agentId: id },
      search: { view: 'chat', panel: 'home' },
    })
  }
}
