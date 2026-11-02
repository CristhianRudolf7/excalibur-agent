export interface Point {
  x: number
  y: number
}
export interface Deliverable {
  id: string
  title: string
  description: string
  agentId: string
  panel: 'files' | 'sheet' | 'document'
  fileId?: string
  spot: Point
  ready?: boolean
}
export interface Room {
  id: string
  label: string
  type: string
  projectId?: string
  mainAgentId?: string
  polygon: Point[]
  spots: Record<string, Point>
  door: Point
  waypoints: Point[]
  background?: string
  interiorSize?: { width: number; height: number }
  interiorSpots: Record<string, Point>
  deliverables: Deliverable[]
  decorate?: boolean
}
export interface WorldLayout {
  id: string
  width: number
  height: number
  background?: string
  fallbackBackground?: string
  rooms: Room[]
  plaza: Point
}
export interface Mission {
  id: string
  title: string
  agentIds: string[]
  roomId: string
  progress: number
  status: 'pending' | 'progress' | 'done' | 'blocked'
}
export function hitPolygon(point: Point, polygon: readonly Point[]): boolean {
  let inside = false
  for (let i = 0, j = polygon.length - 1; i < polygon.length; j = i++) {
    const a = polygon[i]!,
      b = polygon[j]!
    if (
      a.y > point.y !== b.y > point.y &&
      point.x < ((b.x - a.x) * (point.y - a.y)) / (b.y - a.y) + a.x
    )
      inside = !inside
  }
  return inside
}
export const roomAt = (layout: WorldLayout, point: Point) =>
  layout.rooms.find((room) => hitPolygon(point, room.polygon))
