import { create } from 'zustand'
export const useNotice = create<{ message: string; set: (message: string) => void }>((set) => ({
  message: '',
  set: (message) => set({ message }),
}))
export function notifyData(message: string) {
  useNotice.getState().set(message)
}
