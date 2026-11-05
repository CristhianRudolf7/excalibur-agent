import type { LogEntry } from './types'

export function stateRecord(value: unknown): Record<string, unknown> | undefined {
  return value !== null && typeof value === 'object' && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : undefined
}
export function robotState(value: unknown): Record<string, unknown> | undefined {
  const result = stateRecord(value)
  const state = stateRecord(result?.state) ?? result
  return state &&
    (Array.isArray(state.support) || stateRecord(state.gripper) || typeof state.margin === 'number')
    ? state.held === undefined && result && 'held' in result
      ? { ...state, held: result.held }
      : state
    : undefined
}

/** Recorded state is scoped to the selected attempt and never comes from the future. */
export function latestRobotState(
  entries: LogEntry[],
  session: string,
  attempt: number,
  time?: number,
) {
  let latest: { state: Record<string, unknown>; time: number } | undefined
  for (const entry of entries) {
    if (
      entry.session !== session ||
      entry.attempt !== attempt ||
      !Number.isFinite(entry.sim_time) ||
      (time !== undefined && entry.sim_time > time)
    )
      continue
    let state = robotState(entry.state) ?? robotState(entry.result)
    if (!state && entry.tool && entry.summary.trim().startsWith('{')) {
      try {
        state = robotState(JSON.parse(entry.summary))
      } catch {
        /* A human summary carries no structured state. */
      }
    }
    if (state && (!latest || entry.sim_time >= latest.time))
      latest = { state, time: entry.sim_time }
  }
  return latest
}
