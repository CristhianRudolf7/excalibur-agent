import type { FileChange } from '@/lib/rpc/contract'
import { mockChanges } from './mock-data'
import { create } from 'zustand'
interface FilesState {
  loadText: (path: string, content: string) => void
  contents: Record<string, string>
  selected: string
  open: string[]
  changes: Record<string, FileChange>
  select: (path: string, change?: FileChange) => void
  close: (path: string) => void
  edits: Record<string, string>
  edit: (path: string, content: string) => void
}
export const useFiles = create<FilesState>((set) => ({
  contents: {},
  loadText: (path, content) => set((s) => ({ contents: { ...s.contents, [path]: content } })),
  selected: 'src/server/auth.ts',
  open: ['src/server/auth.ts', 'src/tests/auth.test.ts'],
  edits: {},
  changes: mockChanges,
  select: (path, change) =>
    set((s) => ({
      selected: path,
      open: s.open.includes(path) ? s.open : [...s.open, path],
      ...(change
        ? {
            changes: { ...s.changes, [path]: change },
            edits: Object.fromEntries(Object.entries(s.edits).filter(([key]) => key !== path)),
          }
        : {}),
    })),
  close: (path) =>
    set((s) => {
      const open = s.open.filter((p) => p !== path)
      return { open, selected: s.selected === path ? (open.at(-1) ?? '') : s.selected }
    }),
  edit: (path, content) => set((s) => ({ edits: { ...s.edits, [path]: content } })),
}))
