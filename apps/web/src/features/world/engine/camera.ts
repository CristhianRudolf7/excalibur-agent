import type { Point, Room } from '../layout'
export interface Camera extends Point {
  zoom: number
}
export const zoomSteps = [0.75, 1, 1.25, 1.5] as const
export function clampZoom(zoom: number) {
  return Number.isFinite(zoom) ? Math.max(zoomSteps[0], Math.min(zoomSteps.at(-1)!, zoom)) : 1
}
export function snapZoom(zoom: number) {
  const value = clampZoom(zoom)
  return zoomSteps.reduce<number>(
    (best, next) => (Math.abs(next - value) < Math.abs(best - value) ? next : best),
    zoomSteps[0],
  )
}
export function stepZoom(zoom: number, direction: number) {
  const index = zoomSteps.indexOf(snapZoom(zoom) as (typeof zoomSteps)[number])
  return zoomSteps[Math.max(0, Math.min(zoomSteps.length - 1, index + Math.sign(direction)))]!
}
/** Interiors use cover at 75%; larger zoom factors remain centered and bounded, like the campus. */
export function cameraScale(zoom: number, width: number, height: number, room?: Room) {
  if (!room) return zoom
  const size = room.interiorSize ?? { width: 1600, height: 1000 }
  return (Math.max(width / size.width, height / size.height) * zoom) / zoomSteps[0]
}
export function clampCamera(
  camera: Camera,
  width: number,
  height: number,
  map: { width: number; height: number },
  scale = camera.zoom,
): Camera {
  return {
    ...camera,
    x: Math.max(0, Math.min(map.width - Math.min(map.width, width / scale), camera.x)),
    y: Math.max(0, Math.min(map.height - Math.min(map.height, height / scale), camera.y)),
  }
}
export function zoomCamera(
  camera: Camera,
  zoom: number,
  screen: { width: number; height: number },
  bounds: { width: number; height: number },
  room?: Room,
): Camera {
  const before = cameraScale(camera.zoom, screen.width, screen.height, room),
    after = cameraScale(zoom, screen.width, screen.height, room)
  return clampCamera(
    {
      x: camera.x + screen.width / before / 2 - screen.width / after / 2,
      y: camera.y + screen.height / before / 2 - screen.height / after / 2,
      zoom,
    },
    screen.width,
    screen.height,
    bounds,
    after,
  )
}
