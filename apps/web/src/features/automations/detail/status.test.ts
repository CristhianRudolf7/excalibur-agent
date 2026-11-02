import { expect, it } from 'vitest'
import { edgeTraversed, stepStatus } from './status'
import { sampleRun } from './fixtures'
it('maps every step state and defaults missing steps to pending during replay', () => {
  expect(stepStatus(undefined, 's1')).toBeUndefined()
  expect(stepStatus(sampleRun, 'unknown')).toBe('pending')
  for (const status of ['pending', 'running', 'succeeded', 'failed', 'skipped'] as const)
    expect(stepStatus({ ...sampleRun, steps: [{ step_id: 's1', status }] }, 's1')).toBe(status)
})
it('marks only executed paths as traversed', () => {
  expect(edgeTraversed(sampleRun, '__schedule', 's1')).toBe(true)
  expect(edgeTraversed(sampleRun, 's2', 's3')).toBe(true)
  expect(edgeTraversed(sampleRun, 's2', 's4')).toBe(false)
  expect(edgeTraversed(sampleRun, 's4', 's5')).toBe(false)
  expect(edgeTraversed(undefined, 's2', 's3')).toBe(false)
})
