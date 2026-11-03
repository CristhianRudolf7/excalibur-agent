import { create } from 'zustand'
import { extensionCatalog, type ExtensionId } from './catalog'

export const installedExtensionsKey = 'excalibur-installed-extensions-v1'
const defaults = extensionCatalog.map(({ id }) => id)

export function readInstalledExtensions(): ExtensionId[] {
  try {
    const stored: unknown = JSON.parse(localStorage.getItem(installedExtensionsKey) ?? 'null')
    if (Array.isArray(stored) && stored.every((id) => typeof id === 'string'))
      return defaults.filter((id) => stored.includes(id))
  } catch {
    // Unavailable storage or invalid preferences fall back to the built-in catalog.
  }
  return [...defaults]
}

interface ExtensionsState {
  installed: ExtensionId[]
  setInstalled: (id: ExtensionId, installed: boolean) => void
}

export const useExtensions = create<ExtensionsState>((set) => ({
  installed: readInstalledExtensions(),
  setInstalled: (id, installed) =>
    set((state) => {
      const next = defaults.filter((entry) =>
        entry === id ? installed : state.installed.includes(entry),
      )
      try {
        localStorage.setItem(installedExtensionsKey, JSON.stringify(next))
      } catch {
        // The current session remains usable when preferences cannot be saved.
      }
      return { installed: next }
    }),
}))
