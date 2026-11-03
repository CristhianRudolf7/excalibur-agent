import { createFileRoute } from '@tanstack/react-router'
import { GoogleCallback } from '@/features/settings/GoogleCallback'

export const Route = createFileRoute('/oauth/google/callback')({ component: GoogleCallback })
