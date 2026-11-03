import { createFileRoute, redirect } from '@tanstack/react-router'
export const Route = createFileRoute('/coming-soon/$page')({
  beforeLoad: ({ params }) => {
    if (params.page === 'stores' || params.page === 'tools')
      throw redirect({ to: '/extensions', replace: true })
  },
})
