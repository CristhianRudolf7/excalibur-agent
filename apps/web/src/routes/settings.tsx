import { createFileRoute } from '@tanstack/react-router'
export const Route = createFileRoute('/settings')({
  validateSearch: (search: Record<string, unknown>): { section?: string } => ({
    section: typeof search.section === 'string' ? search.section : undefined,
  }),
})
