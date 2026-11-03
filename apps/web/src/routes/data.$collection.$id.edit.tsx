import { createFileRoute } from '@tanstack/react-router'
import { CollectionPage } from '@/features/mydata/MyData'
export const Route = createFileRoute('/data/$collection/$id/edit')({ component: Page })
function Page() {
  return <CollectionPage {...Route.useParams()} mode="edit" />
}
