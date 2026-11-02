import type { SpriteDefinition } from '@/components/pixel-sprites'
export type ActivityKind =
  | 'read'
  | 'search_web'
  | 'run_command'
  | 'edit_file'
  | 'write_doc'
  | 'spreadsheet'
  | 'think'
  | 'wait_user'
  | 'delegate'
  | 'deliver'
  | 'blocked'
  | 'idle'
  | 'reply'
export type ActivityStatus = 'started' | 'progress' | 'completed' | 'waiting' | 'blocked' | 'idle'
export interface WorldAgent {
  id: string
  name: string
  sprite: SpriteDefinition
  homeRoomId: string
  parentId?: string
}
export interface WorldTarget {
  roomId?: string
  projectId?: string
  fileId?: string
  taskId?: string
  deliverableId?: string
}
/** Producer-neutral event. Text is an i18n key unless literal is true; timestamp is epoch milliseconds. */
export interface WorldEvent {
  id: string
  agentId: string
  activity: ActivityKind | (string & {})
  target: WorldTarget
  status: ActivityStatus
  text: string
  timestamp: number
  literal?: boolean
  progress?: number
  agentChanges?: { upsert?: WorldAgent[]; remove?: string[] }
  chatMessageId?: string
}

export const filterActivity = (events: readonly WorldEvent[], roomId?: string) =>
  roomId ? events.filter((event) => event.target.roomId === roomId) : [...events]
