/** Exact circular-arc geometry for the sections' M/H/V/A plotter paths.
 * Native getPointAtLength repeatedly flattens every arc in Chrome. Cache 361
 * analytic points once instead; the shared engine can sample them cheaply.
 * Only section SVG instances are patched, and cleanup restores the methods.
 */
type Point = { x: number; y: number }
type Segment = { length: number; at: (t: number) => Point }
const SAMPLES = 360
export function prepareGeometry(section: HTMLElement) {
  const restores: (() => void)[] = []
  section.querySelectorAll<SVGPathElement>('[data-line-path]').forEach((path) => {
    const tokens = path.getAttribute('d')!.match(/[A-Za-z]|[-+]?\d*\.?\d+(?:e[-+]?\d+)?/g)!
    const segments: Segment[] = []
    let x = 0,
      y = 0,
      cursor = 0
    const number = () => Number(tokens[cursor++]!)
    while (cursor < tokens.length) {
      const command = tokens[cursor++]!
      if (command === 'M') {
        x = number()
        y = number()
        continue
      }
      const startX = x,
        startY = y
      if (command === 'H' || command === 'V') {
        if (command === 'H') x = number()
        else y = number()
        const endX = x,
          endY = y
        segments.push({
          length: Math.hypot(x - startX, y - startY),
          at: (t) => ({ x: startX + (endX - startX) * t, y: startY + (endY - startY) * t }),
        })
      } else if (command === 'A') {
        const radius = number(),
          ry = number(),
          rotation = number(),
          large = number(),
          sweep = number()
        x = number()
        y = number()
        if (radius !== ry || rotation !== 0 || large !== 0)
          throw new Error('Plotter geometry requires short circular arcs')
        const dx = (startX - x) / 2,
          dy = (startY - y) / 2,
          square = dx * dx + dy * dy
        const factor =
          (large === sweep ? -1 : 1) * Math.sqrt(Math.max(0, (radius * radius - square) / square))
        const cx = (startX + x) / 2 + factor * dy,
          cy = (startY + y) / 2 - factor * dx
        const angle = Math.atan2(startY - cy, startX - cx)
        let delta = Math.atan2(y - cy, x - cx) - angle
        if (sweep && delta < 0) delta += Math.PI * 2
        if (!sweep && delta > 0) delta -= Math.PI * 2
        segments.push({
          length: Math.abs(delta) * radius,
          at: (t) => ({
            x: cx + radius * Math.cos(angle + delta * t),
            y: cy + radius * Math.sin(angle + delta * t),
          }),
        })
      } else throw new Error(`Unsupported plotter command: ${command}`)
    }
    const length = segments.reduce((sum, segment) => sum + segment.length, 0)
    const points: Point[] = []
    let segmentIndex = 0,
      start = 0
    for (let i = 0; i <= SAMPLES; i++) {
      const distance = (length * i) / SAMPLES
      while (
        segmentIndex < segments.length - 1 &&
        distance > start + segments[segmentIndex]!.length
      ) {
        start += segments[segmentIndex++]!.length
      }
      const segment = segments[segmentIndex]!
      points.push(segment.at(Math.min(1, Math.max(0, (distance - start) / segment.length))))
    }
    const descriptors = ['getTotalLength', 'getPointAtLength'].map(
      (name) => [name, Object.getOwnPropertyDescriptor(path, name)] as const,
    )
    path.getTotalLength = () => length
    path.getPointAtLength = (distance: number) => {
      const clamped = Math.max(0, Math.min(length, distance))
      const index = (clamped / length) * SAMPLES
      const rounded = Math.round(index)
      if (Math.abs(index - rounded) < 1e-8)
        return new DOMPoint(points[rounded]!.x, points[rounded]!.y)
      // Preserve exact corners for non-grid callers; the shared engine requests
      // the cached grid only, then performs its own frame-time interpolation.
      let start = 0
      for (const segment of segments) {
        if (clamped <= start + segment.length) {
          const point = segment.at((clamped - start) / segment.length)
          return new DOMPoint(point.x, point.y)
        }
        start += segment.length
      }
      return new DOMPoint(points[SAMPLES]!.x, points[SAMPLES]!.y)
    }
    restores.push(() => {
      descriptors.forEach(([name, descriptor]) => {
        if (descriptor) Object.defineProperty(path, name, descriptor)
        else Reflect.deleteProperty(path, name)
      })
    })
  })
  return () => restores.forEach((restore) => restore())
}
