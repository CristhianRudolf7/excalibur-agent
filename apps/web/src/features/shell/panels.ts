import type { PanelView } from '@/features/files/open'
export const panels = [
  'home',
  'activity',
  'changes',
  'files',
  'document',
  'sheet',
  'whiteboard',
  'diagrams',
  'browser',
] as const
export type Panel = PanelView
export function validatePanel(search: Record<string, unknown>): {
  panel: Panel
  credential?: string
  conversation?: string
  view?: 'chat'
} {
  const identifier = (value: unknown) =>
    typeof value === 'string' && /^[a-zA-Z0-9_.:-]{1,200}$/.test(value) ? value : undefined
  const credential = identifier(search.credential)
  return {
    credential,
    conversation: credential ? identifier(search.conversation) : undefined,
    panel:
      search.panel === 'subagents'
        ? 'activity'
        : panels.includes(search.panel as Panel)
          ? (search.panel as Panel)
          : 'home',
    view: credential || search.view === 'chat' ? 'chat' : undefined,
  }
}
