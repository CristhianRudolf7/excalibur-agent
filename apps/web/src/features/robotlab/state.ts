import { create } from 'zustand'
import type { FocusEvent, Scene } from './types'
export interface LabSession {
  session: string
  scene: Scene
}
export const useLab = create<{
  active?: LabSession
  focus?: FocusEvent
  sequence: number
  setActive: (active: LabSession | undefined) => void
}>((set) => ({ sequence: 0, setActive: (active) => set({ active }) }))
