import { describe, expect, it } from 'vitest'
import { previewPixel } from './calibration'

describe('original-frame pixel coordinates', () => {
  it('scales client coordinates relative to the preview, including fractional pixels', () => {
    const rect = { left: 100, top: 50, width: 320, height: 180 }
    const image = { width: 1280, height: 720 }
    expect(previewPixel(180, 95, rect, image)).toEqual([320, 180])
    expect(previewPixel(180.25, 95.5, rect, image)).toEqual([321, 182])
  })

  it('accounts for horizontal and vertical letterboxing and rejects clicks in the bars', () => {
    const image = { width: 640, height: 480 }
    const wide = { left: 20, top: 40, width: 800, height: 400 }
    expect(previewPixel(420, 240, wide, image)).toEqual([320, 240])
    expect(previewPixel(40, 240, wide, image)).toBeNull()
    expect(previewPixel(800, 240, wide, image)).toBeNull()
    const tall = { left: 0, top: 0, width: 320, height: 400 }
    expect(previewPixel(160, 200, tall, image)).toEqual([320, 240])
    expect(previewPixel(160, 40, tall, image)).toBeNull()
    expect(previewPixel(160, 360, tall, image)).toBeNull()
  })

  it('excludes the far edges and invalid geometry instead of submitting out-of-frame pixels', () => {
    const rect = { left: 0, top: 0, width: 320, height: 240 }
    const image = { width: 640, height: 480 }
    expect(previewPixel(0, 0, rect, image)).toEqual([0, 0])
    expect(previewPixel(320, 120, rect, image)).toBeNull()
    expect(previewPixel(100, 240, rect, image)).toBeNull()
    expect(previewPixel(-1, 0, rect, image)).toBeNull()
    expect(previewPixel(NaN, 0, rect, image)).toBeNull()
    expect(previewPixel(0, 0, { ...rect, width: 0 }, image)).toBeNull()
    expect(previewPixel(0, 0, rect, { width: 0, height: 480 })).toBeNull()
  })
})
