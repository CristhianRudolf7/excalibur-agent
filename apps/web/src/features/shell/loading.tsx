import { lazy, type ReactNode } from 'react'
import { useLoading } from '@/components/shared'

/** Match the 150ms reveal delay and 300ms minimum of RPC placeholders. */
export const loadingLazy: typeof lazy = (load) => {
  return lazy(async () => {
    const started = performance.now()
    const result = await load()
    const elapsed = performance.now() - started
    if (elapsed >= 150 && elapsed < 450) {
      await new Promise((resolve) => window.setTimeout(resolve, 450 - elapsed))
    }
    return result
  })
}
export function LoadingFallback({ children }: { children: ReactNode }) {
  const { visible } = useLoading(true)
  return visible ? children : null
}
