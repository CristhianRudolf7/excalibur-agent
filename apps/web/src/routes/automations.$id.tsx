import { createFileRoute } from '@tanstack/react-router'
import { AutomationDetailPage } from '@/features/automations/detail/AutomationDetailPage'
import { AutomationActions } from '@/features/automations/AutomationActions'
export const Route = createFileRoute('/automations/$id')({ component: Detail })
function Detail() {
  const { id } = Route.useParams()
  return <AutomationDetailPage id={id} actions={(a) => <AutomationActions automation={a} />} />
}
