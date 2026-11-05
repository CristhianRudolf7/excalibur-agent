import { gsap } from './index'

/** Helpers return paused animations; pass a factory to onReach to defer all work. */
export function reveal(target: gsap.TweenTarget, vars: gsap.TweenVars = {}) {
  return gsap.fromTo(
    target,
    { opacity: 0 },
    { opacity: 1, duration: 0.35, ease: 'power3.inOut', paused: true, ...vars },
  )
}
export function drawPath(target: SVGPathElement | SVGPathElement[]) {
  const paths = Array.isArray(target) ? target : [target]
  const scales = paths.map((path) => {
    path.setAttribute('pathLength', '1')
    const matrix = path.getScreenCTM()
    let scale = matrix ? Math.hypot(matrix.a, matrix.b) : 1
    if (matrix && Math.abs(scale - Math.hypot(matrix.c, matrix.d)) > 0.0001) {
      const length = path.getTotalLength()
      let previous = path.getPointAtLength(0),
        screenLength = 0,
        sampledLength = 0
      for (let i = 1; i <= 32; i++) {
        const point = path.getPointAtLength((length * i) / 32)
        const dx = point.x - previous.x,
          dy = point.y - previous.y
        sampledLength += Math.hypot(dx, dy)
        screenLength += Math.hypot(matrix.a * dx + matrix.c * dy, matrix.b * dx + matrix.d * dy)
        previous = point
      }
      screenLength += ((length - sampledLength) * (scale + Math.hypot(matrix.c, matrix.d))) / 2
      scale = length ? screenLength / length : 1
    }
    path.style.strokeDasharray = `${scale} ${scale}`
    return scale
  })
  return gsap.fromTo(
    paths,
    { strokeDashoffset: (i: number) => scales[i]! },
    { strokeDashoffset: 0, duration: 0.55, ease: 'power3.inOut', paused: true },
  )
}
/** Measurements are captured once. Targets must share the highlight's offset parent. */
export function steppedHop(highlight: HTMLElement, targets: HTMLElement[]) {
  const origin = highlight.getBoundingClientRect()
  const positions = targets.map((target) => {
    const rect = target.getBoundingClientRect()
    return { x: rect.left - origin.left, y: rect.top - origin.top }
  })
  const timeline = gsap.timeline({ paused: true, defaults: { duration: 0.25, ease: 'steps(1)' } })
  positions.forEach((position) => timeline.to(highlight, position))
  return timeline
}
export function countUp(target: HTMLElement, value: number, decimals = 1, suffix = '') {
  const state = { value: 0 }
  let last = ''
  return gsap.to(state, {
    value,
    duration: 0.7,
    ease: 'power3.inOut',
    paused: true,
    onUpdate() {
      const next = `${state.value.toFixed(decimals)}${suffix}`
      if (next !== last) {
        target.textContent = next
        last = next
      }
    },
  })
}
