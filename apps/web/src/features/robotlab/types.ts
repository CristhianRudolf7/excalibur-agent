import type { RpcEvents, RpcMethods } from '@/lib/rpc/contract'

type WireMethods = Pick<RpcMethods, Extract<keyof RpcMethods, `robotlab.${string}`>>
export type RobotId = 'arm' | 'dog'
export function isRobotId(value: string): value is RobotId {
  return value === 'arm' || value === 'dog'
}
// Local result shapes keep the UI independent of the shared contract generation order.
export type RobotLabMethods = Omit<
  WireMethods,
  | 'robotlab.session.get'
  | 'robotlab.session.list'
  | 'robotlab.robots.list'
  | 'robotlab.skills.list'
  | 'robotlab.skills.read'
  | 'robotlab.session.log'
> & {
  'robotlab.session.get': { params: { session: string }; result: { session: Session } }
  'robotlab.session.list': { params: Record<string, never>; result: { sessions: SessionSummary[] } }
  'robotlab.robots.list': { params: Record<string, never>; result: { robots: RobotCard[] } }
  'robotlab.skills.list': { params: { session: string }; result: { skills: SkillSummary[] } }
  'robotlab.session.log': {
    params: { session: string; attempt?: number; since?: number }
    result: { entries: LogEntry[]; next: number }
  }
  'robotlab.skills.read': { params: { session: string; name: string }; result: { skill: Skill } }
}
type RobotCard = Omit<WireMethods['robotlab.robots.list']['result']['robots'][number], 'id'> & {
  id: RobotId
}
export type RobotLabEvents = Pick<RpcEvents, Extract<keyof RpcEvents, `robotlab.${string}`>>
export type EnvironmentSummary =
  RobotLabMethods['robotlab.env.list']['result']['environments'][number]
export type Environment = RobotLabMethods['robotlab.env.get']['result']['environment']
export type EnvironmentObject = Environment['objects'][number]
export type Session = Omit<
  WireMethods['robotlab.session.get']['result']['session'],
  'robot' | 'goal'
> & { robot: RobotId; goal: Record<string, unknown> }
export type SessionStatus = Session['status']
export type SessionSummary = Omit<
  WireMethods['robotlab.session.list']['result']['sessions'][number],
  'robot'
> & { robot: RobotId }
export type Attempt = Session['attempts'][number]
export type LogEntry = WireMethods['robotlab.session.log']['result']['entries'][number] & {
  state?: Record<string, unknown>
  result?: Record<string, unknown>
}
export interface SkillSummary {
  name: string
  description: string
  version: number
  success_rate: number | null
  uses: number
  failures: number
  updated_at: string
}
export interface Skill extends SkillSummary {
  instructions: string
  versions: {
    version: number
    created_at: string
    accepted: boolean
    success_rate: number | null
    uses: number
  }[]
}
export type LabSettings = RobotLabMethods['robotlab.settings.get']['result']
export type InstallEvent = RobotLabEvents['robotlab.install']
