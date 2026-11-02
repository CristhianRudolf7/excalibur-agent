import { useEffect, useState } from 'react'

/** Delay placeholders by 150ms and retain a displayed placeholder for 300ms. */
export function useLoadingState(pending: boolean) {
  const [shownAt, setShownAt] = useState<number | null>(null)
  useEffect(() => {
    if (pending && shownAt === null) {
      const timer = setTimeout(() => setShownAt(Date.now()), 150)
      return () => clearTimeout(timer)
    }
    if (!pending && shownAt !== null) {
      const timer = setTimeout(() => setShownAt(null), Math.max(0, 300 - (Date.now() - shownAt)))
      return () => clearTimeout(timer)
    }
  }, [pending, shownAt])
  return { visible: shownAt !== null, holding: pending || shownAt !== null }
}

/** Keep lazy content from replacing a fallback midway through its first shimmer. */
export async function loadWithSkeleton<T>(load: () => Promise<T>): Promise<T> {
  const start = Date.now()
  const result = await load()
  const elapsed = Date.now() - start
  if (elapsed >= 150 && elapsed < 450)
    await new Promise((resolve) => setTimeout(resolve, 450 - elapsed))
  return result
}
