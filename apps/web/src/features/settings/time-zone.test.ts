import { afterEach, expect, it, vi } from 'vitest'
import { localClock, timeZones } from './time-zone'

afterEach(() => vi.restoreAllMocks())
it('includes UTC, browser and current aliases alongside the IANA list', () => {
  const zones = timeZones('US/Eastern')
  expect(zones).toContain('UTC')
  expect(zones).toContain('US/Eastern')
  expect(zones).toContain('America/Lima')
  expect(new Set(zones).size).toBe(zones.length)
})
it('uses the fallback when supportedValuesOf is unavailable', () => {
  vi.spyOn(Intl, 'supportedValuesOf').mockImplementation(() => {
    throw new TypeError('unavailable')
  })
  expect(timeZones('Pacific/Chatham')).toEqual(
    expect.arrayContaining(['UTC', 'America/Lima', 'Europe/Madrid', 'Pacific/Chatham']),
  )
})
it('renders local dates and offsets across midnight, DST and fractional offsets', () => {
  const now = new Date('2026-11-04T00:30:00Z')
  expect(localClock('America/Lima', 'en-US', now)).toMatch(/Tue.*11\/03\/2026.*07:30:00.*GMT-05:00/)
  expect(localClock('Europe/Madrid', 'en-GB', now)).toContain('GMT+01:00')
  expect(localClock('Asia/Kathmandu', 'en-GB', now)).toContain('GMT+05:45')
  expect(localClock('Europe/Madrid', 'en-GB', new Date('2026-07-01T12:00:00Z'))).toContain(
    'GMT+02:00',
  )
  expect(localClock('America/Lima', 'es', now)).toContain('GMT-05:00')
})
