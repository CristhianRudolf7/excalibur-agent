import type { Bounds, Viewport } from './minimap-transform'
/** Writes frame geometry without allocating React state or intermediate points. Stroke stays inside map bounds. */
export function updateMinimapViewport(
  node: SVGRectElement,
  viewport: Viewport,
  world: Bounds,
  stroke = 1.5,
) {
  const scale = 220 / world.width
  const width = Math.max(0, Math.min(world.width, viewport.width)),
    height = Math.max(0, Math.min(world.height, viewport.height))
  const x = Math.max(0, Math.min(world.width - width, viewport.x)),
    y = Math.max(0, Math.min(world.height - height, viewport.y))
  node.setAttribute('x', String(x * scale + stroke / 2))
  node.setAttribute('y', String(y * scale + stroke / 2))
  node.setAttribute('width', String(Math.max(0, width * scale - stroke)))
  node.setAttribute('height', String(Math.max(0, height * scale - stroke)))
  node.dataset.worldX = String(x)
  node.dataset.worldY = String(y)
  node.dataset.worldWidth = String(width)
  node.dataset.worldHeight = String(height)
}
