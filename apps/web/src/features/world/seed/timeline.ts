import type { ActivityKind, WorldEvent } from '../events'
import { subagents } from './agents'
import type { ScriptedEvent } from '../simulation'
const scripts: {
  agentId: string
  activity: ActivityKind
  roomId: string
  taskId: string
  text: string
}[] = [
  {
    agentId: 'excalibur',
    activity: 'read',
    roomId: 'library',
    taskId: 'EXC-142',
    text: 'worldSeedRead',
  },
  {
    agentId: 'excalibur',
    activity: 'search_web',
    roomId: 'library',
    taskId: 'EXC-142',
    text: 'worldSeedSearch',
  },
  {
    agentId: 'excalibur',
    activity: 'run_command',
    roomId: 'code',
    taskId: 'EXC-143',
    text: 'worldSeedCommand',
  },
  {
    agentId: 'excalibur',
    activity: 'edit_file',
    roomId: 'code',
    taskId: 'EXC-143',
    text: 'worldSeedEdit',
  },
  {
    agentId: 'excalibur',
    activity: 'write_doc',
    roomId: 'design',
    taskId: 'design-comparison',
    text: 'worldSeedWrite',
  },
  {
    agentId: 'excalibur',
    activity: 'spreadsheet',
    roomId: 'finance',
    taskId: 'forecast',
    text: 'worldSeedSheet',
  },
  {
    agentId: 'excalibur',
    activity: 'think',
    roomId: 'voice',
    taskId: 'EXC-145',
    text: 'worldSeedThink',
  },
  {
    agentId: 'excalibur',
    activity: 'wait_user',
    roomId: 'mail',
    taskId: 'report-mail',
    text: 'worldSeedQuestion',
  },
  {
    agentId: 'excalibur',
    activity: 'delegate',
    roomId: 'launch',
    taskId: 'launch-plan',
    text: 'worldSeedDelegate',
  },
  {
    agentId: 'excalibur',
    activity: 'deliver',
    roomId: 'voice',
    taskId: 'EXC-145',
    text: 'worldReportReady',
  },
  {
    agentId: 'excalibur',
    activity: 'blocked',
    roomId: 'code',
    taskId: 'EXC-143',
    text: 'worldSeedBlocked',
  },
  {
    agentId: 'excalibur',
    activity: 'idle',
    roomId: 'observatory',
    taskId: 'health',
    text: 'worldSeedIdle',
  },
  {
    agentId: 'excalibur',
    activity: 'reply',
    roomId: 'mail',
    taskId: 'report-mail',
    text: 'worldSeedReply',
  },
]
export const LOOP_DURATION = 240000
/** Seeded rotation preserves a fully scripted sequence while providing repeatable test variants. */
export function makeTimeline(seed = 0): ScriptedEvent[] {
  return Array.from({ length: 52 }, (_, index) => {
    const entry = scripts[(((index + seed) % scripts.length) + scripts.length) % scripts.length]!
    const helper =
      entry.activity === 'delegate'
        ? (subagents.find((agent) => agent.parentId === entry.agentId) ?? subagents[0]!)
        : subagents[Math.floor(index / 6) % subagents.length]!
    return {
      offset: 1500 + index * 4400,
      event: {
        id: `script-${index}`,
        agentId: entry.agentId,
        activity: entry.activity,
        target: {
          roomId: entry.roomId,
          taskId: entry.taskId,
          projectId: entry.roomId === 'launch' ? 'launch' : undefined,
          fileId:
            entry.activity === 'read'
              ? 'results/library.md'
              : entry.activity === 'edit_file'
                ? 'src/server/auth.ts'
                : undefined,
          deliverableId: entry.activity === 'deliver' ? 'voice-result' : undefined,
        },
        status:
          entry.activity === 'blocked'
            ? 'blocked'
            : entry.activity === 'wait_user'
              ? 'waiting'
              : entry.activity === 'idle'
                ? 'idle'
                : entry.activity === 'deliver'
                  ? 'completed'
                  : index % 4 === 0
                    ? 'started'
                    : 'progress',
        text: entry.text,
        progress: entry.activity === 'deliver' ? 100 : Math.min(95, 25 + index),
        agentChanges:
          index % 6 === 0 || entry.activity === 'delegate'
            ? { upsert: [helper] }
            : index % 6 === 3
              ? { remove: [subagents[Math.floor((index - 3) / 6) % subagents.length]!.id] }
              : undefined,
      },
    }
  })
}
export const timeline = makeTimeline()
export function initialHistory(now: number): WorldEvent[] {
  return Array.from({ length: 15 }, (_, index) => ({
    ...timeline[index]!.event,
    id: `history-${index}`,
    timestamp: now - (15 - index) * 45000,
  })).reverse()
}
export const initialDialog = (now: number): WorldEvent => ({
  id: 'welcome',
  timestamp: now,
  agentId: 'excalibur',
  activity: 'reply',
  target: { roomId: 'voice' },
  status: 'progress',
  text: 'worldWelcome',
})
