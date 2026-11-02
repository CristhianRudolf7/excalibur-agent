import { afterEach, expect, it, vi } from 'vitest'
import {
  legacySplitRatioKey,
  normalizeSplitRatio,
  readSplitRatio,
  splitRatioFromPanel,
  splitRatioKey,
  splitWidths,
} from './split-layout'

afterEach(() => {
  localStorage.clear()
  vi.restoreAllMocks()
})

it('derives pixel widths from the chat share and keeps both minimum widths', () => {
  expect(splitWidths(1201, 0.5)).toEqual({ chat: 600, panel: 600, available: 1200, overlay: false })
  expect(splitWidths(1201, 0.1).chat).toBe(360)
  expect(splitWidths(1201, 0.9).panel).toBe(320)
  expect(splitWidths(681, 0.8)).toMatchObject({ chat: 360, panel: 320, overlay: false })
  expect(splitWidths(1601, 0.6)).toMatchObject({ chat: 960, panel: 640 })
  expect(splitWidths(1201, Number.NaN).chat).toBe(600)
})

it('uses an overlay when a container cannot fit both columns and the separator', () => {
  for (const width of [0, 320, 680]) {
    expect(splitWidths(width, 0.5)).toEqual({
      chat: width,
      panel: width,
      available: width,
      overlay: true,
    })
    expect(splitRatioFromPanel(400, width)).toBeNull()
  }
  expect(splitRatioFromPanel(400, Number.NaN)).toBeNull()
  expect(splitRatioFromPanel(Number.NaN, 1201)).toBeNull()
})

it('converts a drag to a bounded chat ratio without persisting pixels', () => {
  expect(splitRatioFromPanel(600, 1201)).toBe(0.5)
  expect(splitRatioFromPanel(10, 1201)).toBe(880 / 1200)
  expect(splitRatioFromPanel(2000, 1201)).toBe(360 / 1200)
})

it.each([Number.NaN, Number.POSITIVE_INFINITY, -1, 0, 1, 320])(
  'discards invalid ratios (%s)',
  (value) => {
    expect(normalizeSplitRatio(value)).toBe(0.5)
    for (const key of [legacySplitRatioKey, splitRatioKey]) {
      localStorage.clear()
      localStorage.setItem(key, String(value))
      expect(readSplitRatio()).toBe(0.5)
      expect(localStorage.getItem(splitRatioKey)).toBe('0.5')
      expect(localStorage.getItem(legacySplitRatioKey)).toBeNull()
    }
  },
)

it('migrates a valid legacy panel share once and preserves a current chat share', () => {
  localStorage.setItem(legacySplitRatioKey, '0.25')
  expect(readSplitRatio()).toBe(0.75)
  expect(localStorage.getItem(legacySplitRatioKey)).toBeNull()
  expect(readSplitRatio()).toBe(0.75)
  localStorage.setItem(splitRatioKey, '0.6')
  localStorage.setItem(legacySplitRatioKey, '0.2')
  expect(readSplitRatio()).toBe(0.6)
  expect(localStorage.getItem(legacySplitRatioKey)).toBeNull()
})

it('defaults missing or unavailable storage to an even split', () => {
  expect(readSplitRatio()).toBe(0.5)
  vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
    throw new Error('Unavailable')
  })
  expect(readSplitRatio()).toBe(0.5)
})
