import type { RpcEvents, RpcMethods } from '@/lib/rpc/contract'

export type RobotLabMethods = Pick<RpcMethods, Extract<keyof RpcMethods, `robotlab.${string}`>>
export type RobotLabEvents = Pick<RpcEvents, Extract<keyof RpcEvents, `robotlab.${string}`>>
export type RobotId = Session['robot']
export type EnvironmentSummary =
  RobotLabMethods['robotlab.env.list']['result']['environments'][number]
export type Environment = RobotLabMethods['robotlab.env.get']['result']['environment']
export type EnvironmentObject = Environment['objects'][number]
export type Session = RobotLabMethods['robotlab.session.get']['result']['session']
export type SessionStatus = Session['status']
export type SessionSummary = RobotLabMethods['robotlab.session.list']['result']['sessions'][number]
export type Attempt = Session['attempts'][number]
export type LogEntry = RobotLabMethods['robotlab.session.log']['result']['entries'][number]
export type SkillSummary = RobotLabMethods['robotlab.skills.list']['result']['skills'][number]
export type LabSettings = RobotLabMethods['robotlab.settings.get']['result']
export type InstallEvent = RobotLabEvents['robotlab.install']
