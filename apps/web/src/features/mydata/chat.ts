import { create } from 'zustand'
/** Raw spreadsheet attachments bypass document ingestion; the agent reads the original blob. */
export const useDataAttachment = create<{
  pending?: { agent: string; name: string; digest: string }
  set: (pending?: { agent: string; name: string; digest: string }) => void
}>((set) => ({ set: (pending) => set({ pending }) }))
