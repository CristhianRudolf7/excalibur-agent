import { expect, it } from 'vitest'
import { mapPoint, mapShapeAt, shapeCenter } from './map-geometry'
import { minimapTransform } from './minimap-transform'
import type { MapShape } from './Minimap'
const shape: MapShape = {
  id: 'room',
  label: 'A room',
  points: [
    { x: 200, y: 200 },
    { x: 600, y: 200 },
    { x: 600, y: 600 },
    { x: 200, y: 600 },
  ],
}
it('modal and minimap share the transform for corners, center and arbitrary display sizes', () => {
  const world = { width: 2400, height: 1600 },
    surface = { left: 100, top: 50, width: 900, height: 600 }
  expect(mapPoint({ x: 100, y: 50 }, surface, world)).toEqual({ x: 0, y: 0 })
  expect(mapPoint({ x: 1000, y: 650 }, surface, world)).toEqual({ x: 2400, y: 1600 })
  const center = mapPoint({ x: 550, y: 350 }, surface, world)
  expect(center).toEqual({ x: 1200, y: 800 })
  const mini = minimapTransform(world).toMini(center)
  expect(mini.x).toBeCloseTo(110)
  expect(mini.y).toBeCloseTo((800 * 220) / 2400)
})
it('modal room hit-testing selects configured shapes and leaves empty space navigable', () => {
  expect(shapeCenter(shape)).toEqual({ x: 400, y: 400 })
  expect(mapShapeAt([shape], { x: 400, y: 400 })?.id).toBe('room')
  expect(mapShapeAt([shape], { x: 1000, y: 900 })).toBeUndefined()
  expect(mapShapeAt([{ ...shape, id: 'runtime-project' }], shapeCenter(shape))?.id).toBe(
    'runtime-project',
  )
})
