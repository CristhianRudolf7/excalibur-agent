/** Monotone plotter schedule. Pool adjacent backwards y-runs, so the whole
 * excursion is drawn while it is in view. A small distance weight gives
 * horizontal runs time to draw instead of jumping over them.
 */
export function plotterSchedule(points: { y: number }[], pinTravel = 0, viewport = 0) {
  const blocks: { start: number; count: number; sum: number }[] = []
  points.forEach((point, i) => {
    blocks.push({ start: i, count: 1, sum: point.y })
    while (blocks.length > 1) {
      const b = blocks.at(-1)!,
        a = blocks.at(-2)!
      if (a.sum / a.count <= b.sum / b.count) break
      a.count += b.count
      a.sum += b.sum
      blocks.pop()
    }
  })
  const values = new Array<number>(points.length)
  const first = points[0]!.y,
    last = points.at(-1)!.y
  for (const block of blocks) {
    for (let i = block.start; i < block.start + block.count; i++) {
      const y =
        0.84 * (block.sum / block.count) +
        0.16 * (first + ((last - first) * i) / (points.length - 1))
      // The sticky machine draws slowly through the middle of its canvas while
      // the plates assemble; its reserved native journey needs no frame reads.
      values[i] =
        y + pinTravel * Math.max(0, Math.min(1, (y - viewport * 0.4) / (viewport * 0.2 || 1)))
    }
  }
  const start = values[0]!,
    range = values.at(-1)! - start
  return values.map((value) => (value - start) / (range || 1))
}

export function scheduledProgress(schedule: number[], progress: number) {
  if (!schedule.length || progress <= 0) return 0
  if (progress >= 1) return 1
  let lo = 0,
    hi = schedule.length - 1
  while (hi - lo > 1) {
    const mid = (lo + hi) >>> 1
    if (schedule[mid]! < progress) lo = mid
    else hi = mid
  }
  const between = (progress - schedule[lo]!) / (schedule[hi]! - schedule[lo]! || 1)
  return (lo + between) / (schedule.length - 1)
}
