import { useEffect } from 'react'
import { create } from 'zustand'
import { createJSONStorage, persist } from 'zustand/middleware'
import { subscribeEvents } from '@/lib/rpc/events'
import { openPanelView } from '@/features/files/open'
import type { Artifact, ArtifactEvent, TurnArtifact } from './types'

interface ConversationArtifacts {
  items: TurnArtifact[]
  readyTurns: string[]
  pendingTurns: string[]
  selectedTurn?: string
}
interface State {
  autoOpen: boolean
  conversations: Record<string, ConversationArtifacts>
  setAutoOpen: (value: boolean) => void
  showAll: (session: string) => void
}
export const emptyArtifacts: ConversationArtifacts = {
  items: [],
  readyTurns: [],
  pendingTurns: [],
}
export const useArtifacts = create<State>()(
  persist(
    (set) => ({
      autoOpen: true,
      conversations: {},
      setAutoOpen: (autoOpen) => set({ autoOpen }),
      showAll: (session) =>
        set((state) => ({
          conversations: {
            ...state.conversations,
            [session]: {
              ...(state.conversations[session] ?? emptyArtifacts),
              selectedTurn: undefined,
            },
          },
        })),
    }),
    {
      name: 'excalibur-artifacts',
      storage: createJSONStorage(() => ({
        getItem: (name) => {
          try {
            return localStorage.getItem(name)
          } catch {
            return null
          }
        },
        setItem: (name, value) => {
          try {
            localStorage.setItem(name, value)
          } catch {
            /* Optional browser persistence. */
          }
        },
        removeItem: (name) => {
          try {
            localStorage.removeItem(name)
          } catch {
            /* Optional browser persistence. */
          }
        },
      })),
      // Persist metadata and handled turns so reconnects and reloads cannot reopen a dismissed turn.
      partialize: ({ autoOpen, conversations }) => ({ autoOpen, conversations }),
    },
  ),
)
const compareCreated = (a: Artifact, b: Artifact) =>
  Date.parse(a.created_at) - Date.parse(b.created_at) || a.created_at.localeCompare(b.created_at)

export function receiveArtifactEvent(
  event: ArtifactEvent,
  activeSession: string,
  open: () => void = () => openPanelView('diagrams'),
) {
  const { session_id: session, turn_id: turn } = event.payload
  if (!session || !turn || session !== activeSession) return
  if (event.type !== 'artifact.created' && event.type !== 'artifact.ready') return
  const state = useArtifacts.getState()
  const previous = state.conversations[session] ?? emptyArtifacts
  const ready = event.type === 'artifact.ready'
  const incoming = ready
    ? (event.payload.artifacts ?? [])
    : event.payload.artifact
      ? [event.payload.artifact]
      : []
  const byID = new Map(previous.items.map((item) => [item.id, item]))
  for (const item of incoming) byID.set(item.id, { ...item, turn_id: turn })
  const firstReady = ready && !previous.readyTurns.includes(turn)
  const shouldOpen = firstReady && state.autoOpen && incoming.length > 0
  useArtifacts.setState({
    conversations: {
      ...state.conversations,
      [session]: {
        items: [...byID.values()].sort((a, b) => compareCreated(b, a)),
        readyTurns: firstReady ? [...previous.readyTurns, turn] : previous.readyTurns,
        pendingTurns: ready
          ? previous.pendingTurns.filter((id) => id !== turn)
          : previous.readyTurns.includes(turn) || previous.pendingTurns.includes(turn)
            ? previous.pendingTurns
            : [...previous.pendingTurns, turn],
        selectedTurn: shouldOpen ? turn : previous.selectedTurn,
      },
    },
  })
  if (shouldOpen) open()
}
export function useArtifactEvents(activeSession?: string) {
  useEffect(() => {
    if (!activeSession) return
    return subscribeEvents({
      types: ['artifact.created', 'artifact.ready'],
      event: (event) => {
        if ('session_id' in event.payload)
          receiveArtifactEvent({ type: event.type, payload: event.payload }, activeSession)
      },
      ready: () => {},
      offline: () => {},
    })
  }, [activeSession])
}

/** The first visual produced in a group is its overview; groups never span turns. */
export function groupArtifacts(items: TurnArtifact[]) {
  const groups = new Map<string, TurnArtifact[]>()
  for (const item of items) {
    const key = JSON.stringify([
      item.turn_id,
      item.group ? ['group', item.group] : ['artifact', item.id],
    ])
    const group = groups.get(key) ?? []
    group.push(item)
    groups.set(key, group)
  }
  return [...groups.entries()].map(([key, artifacts]) => ({
    key,
    artifacts: artifacts.sort(compareCreated),
  }))
}
