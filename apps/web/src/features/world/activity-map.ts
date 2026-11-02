import type { WorldEvent } from './events'
import type { WorldLayout, Room } from './layout'
export interface ActivityBehavior {
  roomType: string
  spot: string
  animation: 'read' | 'type' | 'write' | 'think' | 'wait' | 'carry' | 'idle'
  icon: 'working' | 'question' | 'done' | 'blocked' | 'idle'
  dialog: boolean
  activityEntry: boolean
}
export type ActivityMap = Readonly<Record<string, ActivityBehavior>>
const behavior = (
  roomType: string,
  spot: string,
  animation: ActivityBehavior['animation'],
  icon: ActivityBehavior['icon'] = 'working',
  dialog = false,
): ActivityBehavior => ({ roomType, spot, animation, icon, dialog, activityEntry: true })
export const activityMap: ActivityMap = {
  read: behavior('library', 'reading', 'read'),
  search_web: behavior('library', 'desk', 'type'),
  run_command: behavior('code', 'terminal', 'type'),
  edit_file: behavior('code', 'desk', 'type'),
  write_doc: behavior('design', 'desk', 'write'),
  spreadsheet: behavior('finance', 'desk', 'type'),
  think: behavior('voice', 'desk', 'think'),
  wait_user: behavior('mail', 'desk', 'wait', 'question', true),
  delegate: behavior('project', 'door', 'think', 'working', true),
  deliver: behavior('voice', 'delivery', 'carry', 'done', true),
  blocked: behavior('code', 'desk', 'wait', 'blocked', true),
  idle: behavior('garden', 'rest', 'idle', 'idle'),
  reply: behavior('voice', 'desk', 'think', 'working', true),
}
export function behaviorFor(event: WorldEvent, table: ActivityMap = activityMap): ActivityBehavior {
  const base = table[event.activity] ?? table.idle ?? activityMap.idle!
  return {
    ...base,
    icon:
      event.status === 'completed' ? 'done' : event.status === 'blocked' ? 'blocked' : base.icon,
    dialog:
      base.dialog ||
      event.status === 'started' ||
      event.status === 'completed' ||
      event.status === 'blocked',
  }
}
export function destinationFor(
  event: WorldEvent,
  layout: WorldLayout,
  table: ActivityMap = activityMap,
): { room: Room; spot: { x: number; y: number } } | undefined {
  const behavior = behaviorFor(event, table)
  const room =
    layout.rooms.find((room) => room.id === event.target.roomId) ??
    (event.target.projectId
      ? layout.rooms.find((room) => room.projectId === event.target.projectId)
      : undefined) ??
    layout.rooms.find((room) => room.type === behavior.roomType) ??
    layout.rooms[0]
  if (!room) return undefined
  return { room, spot: room.spots[behavior.spot] ?? room.spots.desk ?? room.door }
}
