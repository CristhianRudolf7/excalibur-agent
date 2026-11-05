import { expect, it } from 'vitest'
import { latestRobotState, robotState } from './robotSnapshot'
import type { LogEntry } from './types'

const state = { support: ['FL', 'FR', 'RL'], margin: 0.02, z: 0.27, pitch_deg: 2, roll_deg: -1 }
function entry(time: number, fields: Partial<LogEntry> = {}): LogEntry {
  return {
    id: String(time),
    t: '2026-01-01',
    session: 'test',
    attempt: 2,
    kind: 'act',
    tool: 'sequence',
    summary: 'Moved',
    sim_time: time,
    ...fields,
  }
}
it('reads direct state, state RPC results and action results without depending on generated fields', () => {
  expect(robotState(state)).toEqual(state)
  expect(robotState({ state, attempt: {}, goal: {} })).toEqual(state)
  expect(robotState({ state: { gripper: { opening: 1 } }, held: null })).toEqual({
    gripper: { opening: 1 },
    held: null,
  })
  for (const fields of [
    { state },
    { result: { state, ok: true } },
    { summary: JSON.stringify({ state }) },
  ]) {
    expect(latestRobotState([entry(1, fields)], 'test', 2)?.state).toEqual(state)
  }
})
it('selects the most recent state within the selected attempt and replay time', () => {
  const entries = [
    entry(3, { state }),
    entry(1, { state: { ...state, margin: -0.1 } }),
    entry(4, { state, attempt: 1 }),
    entry(5, { state, session: 'other' }),
    entry(6, { summary: 'Reflection' }),
  ]
  expect(latestRobotState(entries, 'test', 2)?.time).toBe(3)
  expect(latestRobotState(entries, 'test', 2, 2)?.state.margin).toBe(-0.1)
  expect(latestRobotState(entries, 'test', 2, 0)).toBeUndefined()
  expect(latestRobotState(entries, 'test', 1)?.time).toBe(4)
})
it('ignores malformed summaries, missing state, invalid timestamps and unrelated objects', () => {
  expect(robotState(null)).toBeUndefined()
  expect(robotState({ ok: true })).toBeUndefined()
  expect(
    latestRobotState(
      [entry(1, { summary: '{not json' }), entry(2), entry(NaN, { state })],
      'test',
      2,
    ),
  ).toBeUndefined()
})
it('reads the arm gripper, opening and held object', () => {
  const arm = {
    gripper: {
      pos: [0.1, 0.2, 0.9],
      yaw_deg: 20,
      pitch_deg: 0,
      roll_deg: 0,
      opening: 0.5,
      opening_m: 0.0425,
    },
    held: 'cube',
  }
  expect(latestRobotState([entry(1, { result: { state: arm } })], 'test', 2)?.state).toEqual(arm)
})
