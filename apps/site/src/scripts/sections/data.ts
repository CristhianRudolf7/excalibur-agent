import { prepareRailGeometry } from '../../components/landing/sections/parts-a/rail-geometry'
import { createSectionLine, gsap } from '../line'
import { drawPath } from '../line/helpers'

export function init(section: HTMLElement) {
  const restoreGeometry = prepareRailGeometry(section)
  const line = createSectionLine(section)
  section.querySelectorAll('[data-cell]').forEach((cell, index) => {
    line.onReach({ desktop: 0.38 + index * 0.04, phone: 0.27 + index * 0.04 }, () =>
      gsap.fromTo(
        cell,
        { x: -32, opacity: 0 },
        { x: 0, opacity: 1, duration: 0.45, ease: 'power3.inOut', paused: true },
      ),
    )
  })
  line.onReach({ desktop: 0.48, phone: 0.42 }, () =>
    gsap.fromTo(
      section.querySelector('[data-duplicate-fall]'),
      { y: 0, opacity: 1 },
      { y: 18, opacity: 0, duration: 0.5, ease: 'power3.inOut', paused: true },
    ),
  )
  section.querySelectorAll('[data-data-plate]').forEach((plate, index) => {
    line.onReach({ desktop: 0.56 + index * 0.05, phone: 0.49 + index * 0.07 }, () =>
      gsap.fromTo(plate, { y: -24 }, { y: 0, duration: 0.5, ease: 'power3.inOut', paused: true }),
    )
  })
  line.onReach({ desktop: 0.71, phone: 0.72 }, () => {
    if (window.matchMedia('(min-width: 1024px)').matches)
      return gsap
        .timeline({ paused: true })
        .set(section.querySelectorAll('[data-relation]'), { opacity: 1 })
        .add(drawPath([...section.querySelectorAll<SVGPathElement>('[data-relation]')]).play())
    return gsap.timeline({ paused: true })
  })
  line.onReach({ desktop: 0.79, phone: 0.8 }, () =>
    gsap.fromTo(
      section.querySelectorAll('[data-clean-row]'),
      { opacity: 0 },
      { opacity: 1, duration: 0.25, stagger: 0.08, ease: 'steps(1)', paused: true },
    ),
  )
  line.onReach({ desktop: 0.85, phone: 0.87 }, () => {
    const total = section.querySelector<HTMLElement>('[data-total]')!
    const value = { amount: 0 }
    let previous = ''
    return gsap
      .timeline({ paused: true })
      .to(value, {
        amount: 3600,
        duration: 0.7,
        ease: 'power3.inOut',
        onUpdate() {
          const next = `S/ ${Math.round(value.amount).toLocaleString('en-US')}`
          if (next !== previous) {
            total.textContent = next
            previous = next
          }
        },
      })
      .fromTo(
        section.querySelectorAll('[data-bar]'),
        { scaleY: 0 },
        { scaleY: 1, stagger: 0.08, duration: 0.5, ease: 'power3.inOut' },
        0,
      )
  })
  return () => {
    line.destroy()
    restoreGeometry()
  }
}
