import { describe, expect, it } from 'vitest'
import { orderedSessions, validLogImage } from './logic'
import type { SessionSummary } from './types'

describe('lab boundary logic', () => {
  it('sorts live sessions first without changing cached data', () => {
    const sessions = [
      { id: 'old', status: 'stopped', created_at: '2026-01-01' },
      { id: 'new', status: 'succeeded', created_at: '2026-02-01' },
      { id: 'live', status: 'live', created_at: '2025-01-01' },
    ] as SessionSummary[]
    expect(orderedSessions(sessions).map((session) => session.id)).toEqual(['live', 'new', 'old'])
    expect(sessions[0]?.id).toBe('old')
  })
  it('accepts only relative observation and failure PNG paths', () => {
    expect(validLogImage('attempts/1/looks/2.png')).toBe(true)
    expect(validLogImage('attempts/2/failure/image.png')).toBe(true)
    expect(validLogImage('attempts/0/looks/1.png')).toBe(false)
    expect(validLogImage('attempts/1/looks/../secret.png')).toBe(false)
    expect(validLogImage('../../secret')).toBe(false)
    expect(validLogImage('https://example.com/image.png')).toBe(false)
    expect(validLogImage('data:image/png;base64,abc')).toBe(false)
  })
})
