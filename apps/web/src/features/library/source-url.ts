import type { SourceRef } from './types'

export function sourceUrl(source: SourceRef) {
  if (!source.url) return undefined
  try {
    const url = new URL(source.url)
    return url.protocol === 'https:' || url.protocol === 'http:' ? url.href : undefined
  } catch {
    return undefined
  }
}
