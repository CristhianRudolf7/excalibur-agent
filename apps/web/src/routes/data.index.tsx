import { createFileRoute } from '@tanstack/react-router'
import { DataOverview } from '@/features/mydata/MyData'
export const Route = createFileRoute('/data/')({ component: Page })
function Page() {
  return <DataOverview />
}
