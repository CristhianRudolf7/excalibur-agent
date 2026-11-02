import { create } from 'zustand'
import type { DocumentInfo } from './types'

interface LibraryState {
  documents: Record<string, DocumentInfo>
  update: (document: DocumentInfo) => void
  remove: (id: string) => void
}
export const useLibraryStore = create<LibraryState>((set) => ({
  documents: {},
  update: (document) =>
    set((state) => {
      const previous = state.documents[document.id]
      if (previous && Date.parse(previous.updated_at) > Date.parse(document.updated_at))
        return state
      return { documents: { ...state.documents, [document.id]: document } }
    }),
  remove: (id) =>
    set((state) => {
      const documents = { ...state.documents }
      delete documents[id]
      return { documents }
    }),
}))
/** The event bridge can pass library.document_updated payloads here. */
export const documentUpdated = (document: DocumentInfo) =>
  useLibraryStore.getState().update(document)
