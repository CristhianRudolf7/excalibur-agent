import type { Point } from './layout'
import type { Camera } from './engine/camera'
export interface Bounds {
  width: number
  height: number
}
export interface Viewport extends Point, Bounds {}
/** Uniform world/thumbnail transform. The output has the source aspect ratio and no padding. */
export function minimapTransform(world: Bounds, thumbnailWidth = 220) {
  const scale = thumbnailWidth / world.width
  return {
    width: thumbnailWidth,
    height: world.height * scale,
    scale,
    toMini: (point: Point): Point => ({ x: point.x * scale, y: point.y * scale }),
    toWorld: (point: Point): Point => ({ x: point.x / scale, y: point.y / scale }),
  }
}
export function cameraViewport(camera: Camera, screen: Bounds, world: Bounds): Viewport {
  const width = Math.min(world.width, screen.width / camera.zoom),
    height = Math.min(world.height, screen.height / camera.zoom)
  return {
    x: Math.max(0, Math.min(world.width - width, camera.x)),
    y: Math.max(0, Math.min(world.height - height, camera.y)),
    width,
    height,
  }
}
export function minimapViewport(viewport: Viewport, world: Bounds, thumbnailWidth = 220): Viewport {
  const transform = minimapTransform(world, thumbnailWidth)
  const width = Math.max(0, Math.min(world.width, viewport.width)),
    height = Math.max(0, Math.min(world.height, viewport.height))
  const origin = transform.toMini({
    x: Math.max(0, Math.min(world.width - width, viewport.x)),
    y: Math.max(0, Math.min(world.height - height, viewport.y)),
  })
  return { ...origin, width: width * transform.scale, height: height * transform.scale }
}
/** Keeps the stroke entirely inside the exact viewport boundary, including map edges. */
export function viewportStroke(rect: Viewport, stroke = 1.5): Viewport {
  return {
    x: rect.x + stroke / 2,
    y: rect.y + stroke / 2,
    width: Math.max(0, rect.width - stroke),
    height: Math.max(0, rect.height - stroke),
  }
}
