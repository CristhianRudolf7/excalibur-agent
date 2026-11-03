import { useAgents } from '@/features/agents/store'
import { createFileRoute, Navigate } from '@tanstack/react-router'
import { validatePanel } from '@/features/shell/panels'
export const Route = createFileRoute('/')({
  validateSearch: validatePanel,
  component: IndexRedirect,
})
function IndexRedirect() {
  const search = Route.useSearch()
  const favoriteId = useAgents((state) => state.favorite_id)
  const loaded = useAgents((state) => state.loaded)
  if (!loaded) return null
  return <Navigate to="/agents/$agentId" params={{ agentId: favoriteId }} search={search} replace />
}
