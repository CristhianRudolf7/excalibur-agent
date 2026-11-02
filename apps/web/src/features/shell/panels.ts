import type { PanelView } from '@/features/files/open'
export const panels = [
  'home',
  'activity',
  'changes',
  'files',
  'document',
  'sheet',
  'whiteboard',
] as const
export type Panel = PanelView
export function validatePanel(search: Record<string, unknown>): {
  panel: Panel
  view?: 'world' | 'chat'
  room?: string
  worldClock?: 'manual'
  worldDebug?: number
  worldSpeed?: number
} {
  return {
    panel:
      search.panel === 'subagents'
        ? 'activity'
        : panels.includes(search.panel as Panel)
          ? (search.panel as Panel)
          : 'home',
    view: search.view === 'world' ? 'world' : search.view === 'chat' ? 'chat' : undefined,
    room:
      typeof search.room === 'string' && /^[a-zA-Z0-9_-]{1,80}$/.test(search.room)
        ? search.room
        : undefined,
    worldClock: search.worldClock === 'manual' ? 'manual' : undefined,
    worldDebug: Number(search.worldDebug) === 1 ? 1 : undefined,
    worldSpeed:
      Number.isFinite(Number(search.worldSpeed)) && Number(search.worldSpeed) > 0
        ? Number(search.worldSpeed)
        : undefined,
  }
}
