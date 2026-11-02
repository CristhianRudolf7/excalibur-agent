import { createFileRoute, Navigate } from '@tanstack/react-router'
import { validatePanel } from '@/features/shell/panels'
export const Route = createFileRoute('/')({
  validateSearch: validatePanel,
  component: IndexRedirect,
})
function IndexRedirect() {
  const search = Route.useSearch()
  return (
    <Navigate to="/agents/$agentId" params={{ agentId: 'excalibur' }} search={search} replace />
  )
}
