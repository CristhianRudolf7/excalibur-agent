import { create } from 'zustand'

interface LabState {
  agentOpen: boolean
  agentRequest: number
  focusedSession?: string
  openAgent(): void
  closeAgent(): void
  focusSession(session: string): void
}
export const useLab = create<LabState>((set) => ({
  agentOpen: false,
  agentRequest: 0,
  openAgent: () => set((state) => ({ agentOpen: true, agentRequest: state.agentRequest + 1 })),
  closeAgent: () => set({ agentOpen: false }),
  focusSession: (focusedSession) =>
    set((state) => ({ focusedSession, agentOpen: true, agentRequest: state.agentRequest + 1 })),
}))
