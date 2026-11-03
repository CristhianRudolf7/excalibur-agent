import type { RobotImageSize } from '@/lib/rpc/contract'

/** Map a contained preview to the original frame, excluding letterbox bars. */
export function previewPixel(
  x: number,
  y: number,
  rect: Pick<DOMRect, 'left' | 'top' | 'width' | 'height'>,
  image: RobotImageSize,
): [number, number] | null {
  if (
    ![x, y, rect.left, rect.top, rect.width, rect.height, image.width, image.height].every(
      Number.isFinite,
    ) ||
    rect.width <= 0 ||
    rect.height <= 0 ||
    image.width <= 0 ||
    image.height <= 0
  )
    return null
  const scale = Math.min(rect.width / image.width, rect.height / image.height)
  const u = (x - rect.left - (rect.width - image.width * scale) / 2) / scale
  const v = (y - rect.top - (rect.height - image.height * scale) / 2) / scale
  if (u < 0 || v < 0 || u >= image.width || v >= image.height) return null
  return [u, v]
}
