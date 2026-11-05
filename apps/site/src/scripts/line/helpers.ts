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
  paths.forEach((path) => {
    path.setAttribute('pathLength', '1')
    path.style.strokeDasharray = '1 1'
  })
  return gsap.fromTo(
    paths,
    { strokeDashoffset: 1 },
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
