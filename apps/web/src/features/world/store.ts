import { createStore } from 'zustand/vanilla'
import { initialWorldSettings, normalizeSpeed, saveWorldPreference } from './preferences'
import { snapZoom } from './engine/camera'
import { activityMap, behaviorFor, destinationFor, type ActivityMap } from './activity-map'
import type { WorldAgent, WorldEvent } from './events'
import type { Mission, WorldLayout } from './layout'
import type { Camera } from './engine/camera'
export interface WorldState {
  layout: WorldLayout
  agents: Record<string, WorldAgent>
  identities: Record<string, WorldAgent>
  queue: WorldEvent[]
  activity: WorldEvent[]
  missions: Mission[]
  seen: Set<string>
  camera: Camera
  elapsed: number
  speed: number
  zoom: number
  setSpeed: (speed: number) => void
  setZoom: (zoom: number) => void
  emit: (event: WorldEvent) => void
  advance: () => void
  reopen: (event: WorldEvent) => void
  setCamera: (camera: Camera) => void
  setElapsed: (elapsed: number) => void
}
export interface WorldSeed {
  layout: WorldLayout
  agents: WorldAgent[]
  missions: Mission[]
  history?: WorldEvent[]
  dialogs?: WorldEvent[]
}
/** Creates an isolated world session. Its only intake is the generic event contract, independent of producers. */
export function createWorldStore(seed: WorldSeed, table: ActivityMap = activityMap) {
  const settings = initialWorldSettings()
  return createStore<WorldState>((set, get) => ({
    layout: seed.layout,
    identities: Object.fromEntries(seed.agents.map((agent) => [agent.id, agent])),
    agents: Object.fromEntries(seed.agents.map((agent) => [agent.id, agent])),
    queue: seed.dialogs ?? [],
    activity: seed.history ?? [],
    missions: seed.missions.map((mission) => ({ ...mission })),
    seen: new Set([...(seed.history ?? []), ...(seed.dialogs ?? [])].map((event) => event.id)),
    camera: { x: 220, y: 120, zoom: settings.zoom },
    speed: settings.speed,
    zoom: settings.zoom,
    setSpeed: (value) => {
      const speed = normalizeSpeed(value)
      saveWorldPreference('world-speed', speed)
      set({ speed })
    },
    setZoom: (value) => {
      const zoom = snapZoom(value)
      saveWorldPreference('world-zoom', zoom)
      set((state) => ({ zoom, camera: { ...state.camera, zoom } }))
    },
    elapsed: 0,
    emit: (source) => {
      if (get().seen.has(source.id)) return
      const destination = destinationFor(source, get().layout, table)
      const event = {
        ...source,
        target: { ...source.target, roomId: destination?.room.id ?? source.target.roomId },
      }
      const behavior = behaviorFor(event, table)
      set((state) => {
        const agents = { ...state.agents }
        for (const agent of event.agentChanges?.upsert ?? []) agents[agent.id] = agent
        for (const id of event.agentChanges?.remove ?? []) delete agents[id]
        const seen = new Set(state.seen)
        seen.add(event.id)
        if (seen.size > 2000) seen.delete(seen.values().next().value!)
        return {
          agents,
          identities: { ...state.identities, ...agents },
          seen,
          queue: behavior.dialog ? [...state.queue, event].slice(-50) : state.queue,
          activity: behavior.activityEntry
            ? [event, ...state.activity].slice(0, 250)
            : state.activity,
          missions: state.missions.map((mission) =>
            mission.id !== event.target.taskId
              ? mission
              : {
                  ...mission,
                  progress:
                    event.status === 'completed' ? 100 : (event.progress ?? mission.progress),
                  status:
                    event.status === 'completed'
                      ? 'done'
                      : event.status === 'blocked'
                        ? 'blocked'
                        : 'progress',
                },
          ),
          layout: event.target.deliverableId
            ? {
                ...state.layout,
                rooms: state.layout.rooms.map((room) => ({
                  ...room,
                  deliverables: room.deliverables.map((item) =>
                    item.id === event.target.deliverableId
                      ? { ...item, ready: event.status === 'completed' }
                      : item,
                  ),
                })),
              }
            : state.layout,
        }
      })
    },
    advance: () => set((state) => ({ queue: state.queue.slice(1) })),
    reopen: (event) =>
      set((state) => ({ queue: [event, ...state.queue.filter((item) => item.id !== event.id)] })),
    setCamera: (camera) => {
      const zoom = snapZoom(camera.zoom)
      if (zoom !== get().zoom) saveWorldPreference('world-zoom', zoom)
      set({ camera, zoom })
    },
    setElapsed: (elapsed) => set({ elapsed }),
  }))
}
export type WorldStore = ReturnType<typeof createWorldStore>
