import type { KeyboardEvent } from 'react'
export function composerHeight(scrollHeight: number, viewportHeight: number) {
  return Math.max(24, Math.min(scrollHeight, 240, viewportHeight * 0.4))
}
export function shouldSend(
  event: Pick<KeyboardEvent<HTMLTextAreaElement>, 'key' | 'shiftKey' | 'nativeEvent'>,
) {
  return event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing
}
