import '@/features/settings/preferences'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRouter, RouterProvider } from '@tanstack/react-router'
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { routeTree } from './routeTree.gen'
import { ErrorScreen } from '@/components/ErrorScreen'
import '@fontsource-variable/inter'
import './index.css'
import './lib/i18n'

const queryClient = new QueryClient()
const router = createRouter({
  routeTree,
  context: { queryClient },
  defaultErrorComponent: ({ error, reset, info }) => (
    <ErrorScreen
      error={error}
      componentStack={info?.componentStack}
      reset={() => {
        reset()
        void router.invalidate()
      }}
    />
  ),
})

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router
  }
}

const root = document.getElementById('root')
if (!root) throw new Error('missing #root element')

createRoot(root).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  </StrictMode>,
)
