import type { SessionSummary } from './types'

export const maxMediaBytes = 32 * 1024 * 1024
export function validMedia(file: Pick<File, 'type' | 'size'>) {
  return /^(image|video)\//.test(file.type) && file.size > 0 && file.size <= maxMediaBytes
}
export function fileBase64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(String(reader.result).split(',')[1] ?? '')
    reader.onerror = () => reject(reader.error)
    reader.readAsDataURL(file)
  })
}
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
