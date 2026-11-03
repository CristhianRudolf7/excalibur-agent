import { createFileRoute } from '@tanstack/react-router'
import { CollectionPage } from '@/features/mydata/MyData'
export const Route = createFileRoute('/data/$collection/')({ component: Page })
function Page() {
  return <CollectionPage collection={Route.useParams().collection} />
}
