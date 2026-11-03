/** Contract 2 for RB; the bridge and daemon implementation belong to RB1. */
export interface RobotState {
  connected: boolean
  host?: string
  robot_mode?: string
  safety_mode?: string
  program_running?: boolean
  tcp_pose?: number[]
  joints?: number[]
  digital_out?: boolean[]
  digital_in?: boolean[]
  speed_scaling?: number
  gripper?: 'open' | 'closed' | 'unknown'
  updated_at?: string
}

export interface RobotStatus {
  bridge: 'running' | 'stopped' | 'error'
  state?: RobotState
  camera?: { updated_at: string; width: number; height: number }
  error?: string
}

export type ConnectionStatus = 'disconnected' | 'connecting' | 'connected' | 'error'

export function connectionStatus(
  status?: RobotStatus,
  connecting = false,
  failed = false,
): ConnectionStatus {
  if (connecting) return 'connecting'
  if (failed || status?.bridge === 'error') return 'error'
  return status?.state?.connected ? 'connected' : 'disconnected'
}

export function robotEventState(payload: unknown): RobotState | undefined {
  if (!payload || typeof payload !== 'object' || !('state' in payload)) return
  const state = payload.state
  if (!state || typeof state !== 'object' || !('connected' in state)) return
  if (typeof state.connected !== 'boolean') return
  return state as RobotState
}

declare module '@/lib/rpc/contract' {
  interface RpcMethods {
    'robot.status': { params: Record<string, never>; result: RobotStatus }
    'robot.connect': { params: { host: string }; result: RobotStatus }
    'robot.disconnect': { params: Record<string, never>; result: RobotStatus }
    'robot.stop': { params: Record<string, never>; result: { ok: boolean; error?: string } }
    'robot.frame': {
      params: { blob: string; width: number; height: number }
      result: { ok: boolean; error?: string }
    }
  }
}
