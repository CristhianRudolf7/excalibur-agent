import { createFileRoute } from '@tanstack/react-router'
import { validatePanel } from '@/features/shell/panels'
export const Route = createFileRoute('/agents/$agentId')({ validateSearch: validatePanel })
