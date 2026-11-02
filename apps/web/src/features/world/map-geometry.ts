import { hitPolygon, type Point } from './layout'
import { minimapTransform, type Bounds } from './minimap-transform'
import type { MapShape } from './Minimap'
/** Converts a click on an aspect-correct map surface with the same transform used by the minimap. */
export function mapPoint(
  point: Point,
  surface: { left: number; top: number; width: number; height: number },
  world: Bounds,
): Point {
  const transform = minimapTransform(world)
  return transform.toWorld({
    x: ((point.x - surface.left) / surface.width) * transform.width,
    y: ((point.y - surface.top) / surface.height) * transform.height,
  })
}
export const mapShapeAt = (shapes: readonly MapShape[], point: Point) =>
  shapes.find((shape) => hitPolygon(point, shape.points))
export function shapeCenter(shape: MapShape): Point {
  if (!shape.points.length) return { x: 0, y: 0 }
  return {
    x: shape.points.reduce((sum, point) => sum + point.x, 0) / shape.points.length,
    y: shape.points.reduce((sum, point) => sum + point.y, 0) / shape.points.length,
  }
}
