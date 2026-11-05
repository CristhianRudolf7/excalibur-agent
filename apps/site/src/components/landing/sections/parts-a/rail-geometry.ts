/**
 * Local adapter for the shared engine's synchronous 361-point sampling pass.
 * Native getPointAtLength repeatedly flattens SVG arcs in Chrome (400ms at 4x).
 * These rails contain only straight segments and exact quarter-circle arcs;
 * evaluate them analytically, without changing the SVG or the engine contract.
 * Methods are installed on these path instances only, never on a prototype.
 */
interface Point {
  x: number
  y: number
}
interface Segment {
  start: number
  length: number
  at: (t: number) => Point
}

function geometry(source: string) {
  const tokens = source.match(/[MHVA]|-?\d*\.?\d+(?:e[-+]?\d+)?/gi) ?? []
  const segments: Segment[] = []
  let index = 0,
    cursor = { x: 0, y: 0 },
    length = 0
  const number = () => Number(tokens[index++]!)
  while (index < tokens.length) {
    const command = tokens[index++]!
    const from = { ...cursor }
    if (command === 'M') {
      cursor = { x: number(), y: number() }
      continue
    }
    if (command === 'H' || command === 'V') {
      cursor = command === 'H' ? { x: number(), y: from.y } : { x: from.x, y: number() }
      const to = { ...cursor },
        distance = Math.hypot(to.x - from.x, to.y - from.y)
      segments.push({
        start: length,
        length: distance,
        at: (t) => ({ x: from.x + (to.x - from.x) * t, y: from.y + (to.y - from.y) * t }),
      })
      length += distance
    } else if (command === 'A') {
      const radius = number(),
        ry = number(),
        rotation = number(),
        large = number(),
        sweep = number()
      cursor = { x: number(), y: number() }
      const to = { ...cursor }
      // Fail open to native geometry if future artwork changes the rail vocabulary.
      if (
        radius !== ry ||
        rotation ||
        large ||
        Math.abs(Math.abs(to.x - from.x) - radius) > 0.001 ||
        Math.abs(Math.abs(to.y - from.y) - radius) > 0.001
      )
        return
      const first = { x: from.x, y: to.y }
      const positive =
        (from.x - first.x) * (to.y - first.y) - (from.y - first.y) * (to.x - first.x) > 0
      const center = positive === !!sweep ? first : { x: to.x, y: from.y }
      const angle = Math.atan2(from.y - center.y, from.x - center.x),
        turn = ((sweep ? 1 : -1) * Math.PI) / 2
      const distance = (radius * Math.PI) / 2
      segments.push({
        start: length,
        length: distance,
        at: (t) => ({
          x: center.x + radius * Math.cos(angle + turn * t),
          y: center.y + radius * Math.sin(angle + turn * t),
        }),
      })
      length += distance
    } else return
  }
  if (!segments.length) return
  return {
    length,
    at(distance: number) {
      const value = Math.max(0, Math.min(length, distance))
      const segment = segments.find((item) => value <= item.start + item.length) ?? segments.at(-1)!
      const point = segment.at(segment.length ? (value - segment.start) / segment.length : 0)
      return new DOMPoint(point.x, point.y)
    },
  }
}

export function prepareRailGeometry(section: HTMLElement) {
  const cleanups: (() => void)[] = []
  section.querySelectorAll<SVGPathElement>('[data-line-path]').forEach((path) => {
    const rail = geometry(path.getAttribute('d') ?? '')
    if (!rail) return
    const descriptors = ['getTotalLength', 'getPointAtLength'].map(
      (name) => [name, Object.getOwnPropertyDescriptor(path, name)] as const,
    )
    Object.defineProperties(path, {
      getTotalLength: { configurable: true, value: () => rail.length },
      getPointAtLength: { configurable: true, value: (distance: number) => rail.at(distance) },
    })
    cleanups.push(() =>
      descriptors.forEach(([name, descriptor]) => {
        if (descriptor) Object.defineProperty(path, name, descriptor)
        else Reflect.deleteProperty(path, name)
      }),
    )
  })
  return () => cleanups.forEach((cleanup) => cleanup())
}
