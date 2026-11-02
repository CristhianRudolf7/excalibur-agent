import { afterEach, expect, it, vi } from 'vitest'
import { manualWorldClock, publishWorldDebug } from './debug'

const originalLocation = window.location.href
afterEach(() => {
  vi.unstubAllEnvs()
  window.history.replaceState(null, '', originalLocation)
  delete window.__excaliburWorld
})

it('keeps manual clocks and debug controls unavailable in production without explicit diagnostics', () => {
  vi.stubEnv('MODE', 'production')
  window.history.replaceState(null, '', '?worldClock=manual')
  expect(manualWorldClock()).toBe(false)
  publishWorldDebug({ running: false, fps: 0, mode: 'chat', ready: false })
  expect(window.__excaliburWorld).toBeUndefined()
})

it('requires an explicit manual clock even when diagnostics are enabled', () => {
  vi.stubEnv('MODE', 'production')
  window.history.replaceState(null, '', '?worldDebug=1')
  expect(manualWorldClock()).toBe(false)
  window.history.replaceState(null, '', '?worldDebug=1&worldClock=manual')
  expect(manualWorldClock()).toBe(true)
  const advance = vi.fn()
  publishWorldDebug({
    running: true,
    fps: 30,
    mode: 'campus',
    ready: true,
    camera: { x: 1, y: 2, zoom: 1 },
    advance,
  })
  window.__excaliburWorld!.advance!(100)
  expect(advance).toHaveBeenCalledWith(100)
})

it('permits diagnostics and a manual clock in development', () => {
  vi.stubEnv('MODE', 'development')
  window.history.replaceState(null, '', '?worldClock=manual')
  expect(manualWorldClock()).toBe(true)
})
