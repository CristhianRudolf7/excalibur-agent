import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { rpc } from '@/lib/rpc/client'
import { browserVoiceSupported } from './speech'
import { voiceMode } from './callStore'

vi.mock('./speech', () => ({ browserVoiceSupported: vi.fn(), speechLang: () => 'en-US' }))
vi.mock('./call', () => ({ VoiceCall: vi.fn() }))
beforeEach(() => {
  vi.mocked(browserVoiceSupported).mockReturnValue(true)
})
afterEach(() => vi.restoreAllMocks())
it('prefers realtime whenever voice status is available', async () => {
  vi.spyOn(rpc, 'call').mockResolvedValue({ available: true, voices: [] })
  expect(await voiceMode()).toBe('realtime')
})
it('uses browser speech when the daemon is unavailable', async () => {
  vi.spyOn(rpc, 'call').mockRejectedValue(new Error('offline'))
  expect(await voiceMode()).toBe('browser')
})
it('reports unavailable without credentials or browser speech', async () => {
  vi.spyOn(rpc, 'call').mockResolvedValue({ available: false, voices: [] })
  vi.mocked(browserVoiceSupported).mockReturnValue(false)
  expect(await voiceMode()).toBeUndefined()
})
