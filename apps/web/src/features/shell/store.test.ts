import { describe, it, expect, vi } from 'vitest'
import { readPreference, savePreference } from './store'
describe('shell preferences', () => {
  it('tolerates unavailable storage', () => {
    const spy = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('Unavailable')
    })
    expect(() => savePreference('test', 1)).not.toThrow()
    spy.mockRestore()
    expect(readPreference('missing', 0.5)).toBe(0.5)
  })
})
