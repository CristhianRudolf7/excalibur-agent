import type { ActivityBehavior } from '../activity-map'
import type { WorldAgent } from '../events'
import type { Point, Room, WorldLayout } from '../layout'
import type { Camera } from './camera'
export interface AgentState {
  agent: WorldAgent
  position: Point
  roomId: string
  route: Point[]
  waypoint: number
  animation: ActivityBehavior['animation']
  icon: ActivityBehavior['icon']
  task: string
  frame: number
}
export interface WorldRenderer {
  mount: (host: HTMLElement, tick: (delta: number, actualElapsed?: number) => void) => Promise<void>
  unmount: () => void
  setLayout: (layout: WorldLayout, room?: Room) => Promise<void>
  upsertAgent: (state: AgentState) => void
  removeAgent: (id: string) => void
  moveAgent: (state: AgentState) => void
  setCamera: (camera: Camera, room?: Room) => void
  highlight: (room?: Room) => void
  render: () => void
  start: () => void
  stop: () => void
  resize: (width: number, height: number) => void
}
