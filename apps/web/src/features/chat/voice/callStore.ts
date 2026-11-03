import { create } from 'zustand'
import { rpc } from '@/lib/rpc/client'
import i18n from '@/lib/i18n'
import { VoiceCall, type CallSnapshot, type VoiceMode } from './call'
import { browserVoiceSupported, speechLang } from './speech'

const idle: CallSnapshot = { state: 'ready', energy: 0, transcript: [] }

interface CallStore {
  snapshot: CallSnapshot
  /** The call keeps running while the overlay is hidden. */
  overlay: boolean
  connecting: boolean
  unavailable: boolean
  muted: boolean
  speaker: boolean
  startedAt?: number
  /** Opens the overlay; starts a call unless one is already live. */
  open: () => void
  hide: () => void
  hangup: () => void
  toggleMute: () => void
  toggleSpeaker: () => void
}

let call: VoiceCall | undefined
let attempt = 0

export function callActive(snapshot: CallSnapshot) {
  return snapshot.state !== 'ready' && snapshot.state !== 'ended'
}

async function voiceMode(): Promise<VoiceMode | undefined> {
  const server = await rpc
    .call('voice.status', undefined, { timeoutMs: 8000 })
    .then((status) => status.available)
    .catch(() => false)
  // Without voice credentials the call runs on the browser's own STT and TTS.
  return server ? 'server' : browserVoiceSupported() ? 'browser' : undefined
}

export const useCall = create<CallStore>((set, get) => ({
  snapshot: idle,
  overlay: false,
  connecting: false,
  unavailable: false,
  muted: false,
  speaker: true,
  open: () => {
    set({ overlay: true })
    const { snapshot, connecting } = get()
    if (connecting || callActive(snapshot)) return
    const id = ++attempt
    call?.end()
    call = undefined
    set({
      connecting: true,
      unavailable: false,
      muted: false,
      speaker: true,
      snapshot: idle,
      startedAt: undefined,
    })
    void voiceMode().then(async (mode) => {
      if (id !== attempt) return
      if (!mode) {
        set({ connecting: false, unavailable: true })
        return
      }
      const instance = new VoiceCall(
        (next) => {
          if (call !== instance) return
          set((state) => ({
            snapshot: next,
            startedAt: callActive(next) ? (state.startedAt ?? Date.now()) : state.startedAt,
          }))
        },
        { mode, lang: speechLang(i18n.language) },
      )
      call = instance
      await instance.start()
      if (id === attempt) set({ connecting: false })
    })
  },
  hide: () => set({ overlay: false }),
  hangup: () => {
    attempt++
    call?.end()
    call = undefined
    set({ overlay: false, connecting: false, snapshot: idle, startedAt: undefined })
  },
  toggleMute: () => {
    const muted = !get().muted
    call?.mute(muted)
    set({ muted })
  },
  toggleSpeaker: () => {
    const speaker = !get().speaker
    call?.setSpeaker(speaker)
    set({ speaker })
  },
}))
