import { afterEach, expect, it, vi } from 'vitest'
import {
  clampSidebarWidth,
  defaultSidebarWidth,
  legacySidebarWidthKey,
  readSidebarWidth,
  saveSidebarWidth,
  sidebarWidthKey,
  sidebarWidthLimit,
} from './sidebar-size'

afterEach(() => {
  localStorage.clear()
  vi.restoreAllMocks()
})
it('bounds desktop widths and honors the viewport limit', () => {
  expect(clampSidebarWidth(100, 1280)).toBe(200)
  expect(clampSidebarWidth(500, 1280)).toBe(400)
  expect(clampSidebarWidth(360, 800)).toBe(320)
  expect(sidebarWidthLimit(1536)).toBe(400)
})
it('persists a custom width and migrates a legacy width', () => {
  expect(saveSidebarWidth(312, 1280)).toBe(312)
  expect(readSidebarWidth()).toBe(312)
  localStorage.clear()
  localStorage.setItem(legacySidebarWidthKey, '280')
  expect(readSidebarWidth()).toBe(280)
  expect(localStorage.getItem(legacySidebarWidthKey)).toBeNull()
  expect(localStorage.getItem(sidebarWidthKey)).toBe('280')
})
it.each(['NaN', 'Infinity', '0', '-1', '199', '401', '{}', ''])(
  'discards invalid persisted size %s',
  (raw) => {
    localStorage.setItem(sidebarWidthKey, raw)
    expect(readSidebarWidth()).toBe(defaultSidebarWidth)
    expect(localStorage.getItem(sidebarWidthKey)).toBe('240')
  },
)
it('ignores unmeasured and invalid writes without overwriting the preference', () => {
  saveSidebarWidth(300, 1280)
  for (const [width, viewport] of [
    [0, 1280],
    [Number.NaN, 1280],
    [300, 0],
    [300, Number.NaN],
  ])
    expect(saveSidebarWidth(width!, viewport!)).toBeNull()
  expect(readSidebarWidth()).toBe(300)
})
it('tolerates unavailable storage and retains a usable in-memory size', () => {
  vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
    throw new Error('Unavailable')
  })
  vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
    throw new Error('Unavailable')
  })
  expect(readSidebarWidth()).toBe(240)
  expect(saveSidebarWidth(320, 1280)).toBe(320)
})
