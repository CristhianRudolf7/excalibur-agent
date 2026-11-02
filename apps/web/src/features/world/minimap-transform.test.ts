import { describe, expect, it } from 'vitest'
import {
  cameraViewport,
  minimapTransform,
  minimapViewport,
  viewportStroke,
} from './minimap-transform'
describe('world to minimap transform', () => {
  it('maps all corners and center with the campus aspect ratio and no padding', () => {
    const transform = minimapTransform({ width: 2400, height: 1600 }, 240)
    expect(transform.width).toBe(240)
    expect(transform.height).toBe(160)
    expect(transform.toMini({ x: 0, y: 0 })).toEqual({ x: 0, y: 0 })
    expect(transform.toMini({ x: 2400, y: 0 })).toEqual({ x: 240, y: 0 })
    expect(transform.toMini({ x: 0, y: 1600 })).toEqual({ x: 0, y: 160 })
    expect(transform.toMini({ x: 2400, y: 1600 })).toEqual({ x: 240, y: 160 })
    expect(transform.toMini({ x: 1200, y: 800 })).toEqual({ x: 120, y: 80 })
    expect(transform.toWorld({ x: 120, y: 80 })).toEqual({ x: 1200, y: 800 })
  })
  it('matches the real camera viewport and keeps its complete stroke inside the thumbnail', () => {
    const world = { width: 2400, height: 1600 },
      viewport = cameraViewport({ x: 300, y: 200, zoom: 1.5 }, { width: 1200, height: 900 }, world)
    expect(viewport).toEqual({ x: 300, y: 200, width: 800, height: 600 })
    expect(minimapViewport(viewport, world, 240)).toEqual({ x: 30, y: 20, width: 80, height: 60 })
    const edge = minimapViewport({ x: 9999, y: -100, width: 800, height: 600 }, world, 240)
    expect(edge).toEqual({ x: 160, y: 0, width: 80, height: 60 })
    const stroke = viewportStroke(edge)
    expect(stroke.x + stroke.width + 0.75).toBe(240)
    expect(stroke.y - 0.75).toBe(0)
  })
})
