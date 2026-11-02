import { create } from 'zustand'
import type { TFunction } from 'i18next'
import type { Automation } from '@/lib/rpc/contract'
import { describeSchedule } from './api'
export const useAutomationContext = create<{
  automation?: Automation
  draft: string
  attach: (automation: Automation) => void
  prefill: (draft: string) => void
  clear: () => void
}>((set) => ({
  draft: '',
  attach: (automation) => set({ automation, draft: '' }),
  prefill: (draft) => set({ draft, automation: undefined }),
  clear: () => set({ automation: undefined, draft: '' }),
}))
export function automationPrefix(
  text: string,
  automation: Automation | undefined,
  t: TFunction,
  locale: string,
) {
  if (!automation) return text
  const spanish = locale.startsWith('es')
  return `[${t('autoAttached')}: id=${automation.id} ${spanish ? 'versión' : 'version'}=${automation.version} ${spanish ? 'nombre' : 'name'}=«${automation.name}»]\n${describeSchedule(automation.schedule, t, locale)} (${automation.schedule.timezone})\n${automation.steps.map((s) => `${s.id} · ${s.type} · ${s.title}`).join('\n')}\n[/automation]\n\n${text}`
}
/** Preserve a readable chip in persisted bubbles while keeping the full context on the wire. */
export function visibleAutomationMessage(text: string, t: TFunction) {
  return text.replace(
    /^\[(?:Automatización adjunta|Attached automation):[^\n]*?(?:nombre|name)=«([^\n]*)»\]\n[\s\S]*?\n\[\/automation\]\n\n/,
    (_match, name: string) => `📎 ${t('autoBubble', { name })}\n\n`,
  )
}
