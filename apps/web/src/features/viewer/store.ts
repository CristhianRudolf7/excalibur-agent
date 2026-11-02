import { create } from 'zustand'

/** A request to show one library document in the right panel, optionally at a cited location. */
export interface ViewerTarget {
  documentId: string
  /** Chunk to highlight; the viewer loads its boxes and page through library.chunk. */
  chunkId?: string
  /** Physical 1-based page to open when no chunk is given. */
  page?: number
}

export type WorkspacePreview =
  | { status: 'loading'; path: string }
  | { status: 'error' | 'unsupported'; path: string }
  | { status: 'image'; path: string; url: string }

interface ViewerState {
  workspace: WorkspacePreview | null
  setWorkspace: (workspace: WorkspacePreview | null) => void
  target: ViewerTarget | null
  open: (target: ViewerTarget) => void
  close: () => void
}

export const useViewerStore = create<ViewerState>((set) => ({
  workspace: null,
  setWorkspace: (workspace) => set({ workspace }),
  target: null,
  open: (target) => set({ target: { ...target }, workspace: null }),
  close: () => set({ target: null, workspace: null }),
}))
