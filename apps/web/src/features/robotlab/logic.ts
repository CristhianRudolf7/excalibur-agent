import type { SessionSummary } from './types'

export function orderedSessions(sessions: SessionSummary[]) {
  return [...sessions].sort(
    (a, b) =>
      Number(b.status === 'live') - Number(a.status === 'live') ||
      b.created_at.localeCompare(a.created_at),
  )
}
export function validLogImage(path: string) {
  return /^attempts\/[1-9]\d*\/(?:looks\/[a-zA-Z0-9_-]+\.png|failure\/image\.png)$/.test(path)
}
