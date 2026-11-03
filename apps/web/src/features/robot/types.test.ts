import { describe, expect, it } from 'vitest'
import { connectionStatus, robotEventState } from './types'

describe('Robot connection status', () => {
  it('maps unavailable, disconnected, pending, connected and bridge failure states', () => {
    expect(connectionStatus()).toBe('disconnected')
    expect(connectionStatus({ bridge: 'stopped' })).toBe('disconnected')
    expect(connectionStatus({ bridge: 'running', state: { connected: false } })).toBe(
      'disconnected',
    )
    expect(connectionStatus(undefined, true)).toBe('connecting')
    expect(connectionStatus({ bridge: 'running', state: { connected: true } })).toBe('connected')
    expect(connectionStatus({ bridge: 'error', state: { connected: true } })).toBe('error')
    expect(connectionStatus(undefined, false, true)).toBe('error')
    expect(connectionStatus(undefined, true, true)).toBe('connecting')
  })

  it('accepts state envelopes and ignores malformed live events', () => {
    const state = { connected: true, tcp_pose: [0.1, 0.2, 0.3, 0, 0, 0] }
    expect(robotEventState({ state })).toEqual(state)
    for (const payload of [
      null,
      {},
      { state: null },
      { state: 'RUNNING' },
      { state: { connected: 'yes' } },
    ])
      expect(robotEventState(payload)).toBeUndefined()
  })
})
