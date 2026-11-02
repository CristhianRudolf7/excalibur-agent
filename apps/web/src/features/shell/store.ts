import { create } from 'zustand'
import { readSidebarWidth, saveSidebarWidth } from './sidebar-size'
import { normalizeSplitRatio, readSplitRatio, splitRatioKey } from './split-layout'
export function readPreference(key: string, fallback: number) {
  try {
    const value = localStorage.getItem(key)
    return value === null || !Number.isFinite(Number(value)) ? fallback : Number(value)
  } catch {
    return fallback
  }
}
export function savePreference(key: string, value: number) {
  try {
    localStorage.setItem(key, String(value))
  } catch {
    /* Storage can be unavailable in private contexts. */
  }
}
interface ShellState {
  collapsed: boolean
  drawer: boolean
  panelOpen: boolean
  ratio: number
  sidebarWidth: number
  setSidebarWidth: (width: number, viewport: number) => void
  toggleSidebar: (mobile: boolean) => void
  setDrawer: (open: boolean) => void
  togglePanel: () => void
  openPanel: () => void
  setRatio: (ratio: number) => void
}
export const useShell = create<ShellState>((set) => ({
  collapsed: readPreference('sidebar-collapsed', 0) === 1,
  drawer: false,
  panelOpen: false,
  ratio: readSplitRatio(),
  sidebarWidth: readSidebarWidth(),
  setSidebarWidth: (width, viewport) => {
    const next = saveSidebarWidth(width, viewport)
    if (next !== null) set({ sidebarWidth: next })
  },
  toggleSidebar: (mobile) =>
    set((s) => {
      if (mobile) return { drawer: !s.drawer }
      savePreference('sidebar-collapsed', Number(!s.collapsed))
      return { collapsed: !s.collapsed }
    }),
  setDrawer: (drawer) => set({ drawer }),
  togglePanel: () => set((s) => ({ panelOpen: !s.panelOpen })),
  openPanel: () => set({ panelOpen: true }),
  setRatio: (ratio) => {
    const next = normalizeSplitRatio(ratio)
    savePreference(splitRatioKey, next)
    set({ ratio: next })
  },
}))
