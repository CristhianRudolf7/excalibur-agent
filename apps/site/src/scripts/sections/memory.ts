import { prepareRailGeometry } from '../../components/landing/sections/parts-a/rail-geometry'
import { createSectionLine, gsap } from '../line'
import { drawPath } from '../line/helpers'

export function init(section: HTMLElement) {
  const restoreGeometry = prepareRailGeometry(section)
  const line = createSectionLine(section)
  section.querySelectorAll('[data-memory-plate]').forEach((plate, index) => {
    line.onReach({ desktop: 0.12 + index * 0.1, phone: 0.26 + index * 0.13 }, () =>
      gsap
        .timeline({ paused: true })
        .to(plate, { y: -14, duration: 0.25, ease: 'power3.inOut' })
        .to(plate, { y: 0, duration: 0.35, ease: 'power3.inOut' }),
    )
  })
  line.onReach({ desktop: 0.49, phone: 0.7 }, () =>
    gsap.fromTo(
      section.querySelector('[data-memory-page]'),
      { y: -28, opacity: 0 },
      { y: 0, opacity: 1, duration: 0.55, ease: 'power3.inOut', paused: true },
    ),
  )
  line.onReach({ desktop: 0.67, phone: 0.77 }, () =>
    gsap.fromTo(
      section.querySelector('[data-memory-highlight]'),
      { scaleX: 0 },
      { scaleX: 1, duration: 0.55, ease: 'power3.inOut', paused: true },
    ),
  )
  line.onReach({ desktop: 0.75, phone: 0.84 }, () => {
    const timeline = gsap.timeline({ paused: true })
    if (window.matchMedia('(min-width: 1024px)').matches)
      timeline
        .set(section.querySelector('[data-citation-leader]'), { opacity: 1 })
        .add(drawPath(section.querySelector<SVGPathElement>('[data-citation-leader]')!).play())
    return timeline.fromTo(
      section.querySelector('[data-memory-citation]'),
      { opacity: 0 },
      { opacity: 1, duration: 0.25 },
    )
  })
  return () => {
    line.destroy()
    restoreGeometry()
  }
}
