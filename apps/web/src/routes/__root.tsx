import { Shell } from '@/features/shell/Shell'
import type { QueryClient } from '@tanstack/react-query'
import { createRootRouteWithContext, Outlet, useLocation } from '@tanstack/react-router'

export interface RouterContext {
  queryClient: QueryClient
}

export const Route = createRootRouteWithContext<RouterContext>()({
  component: Root,
})

function Root() {
  const location = useLocation()
  return location.pathname === '/oauth/google/callback' ? <Outlet /> : <Shell />
}
