import { createSectionLine, gsap } from '../line'
import { drawPath } from '../line/helpers'

export function init(section: HTMLElement) {
  const line = createSectionLine(section, { intro: true, start: 'top top', end: 'bottom 35%' })
  // Factories run when the pen arrives, inside the active responsive composition.
  const visibleSvg = () =>
    section.querySelector<SVGSVGElement>(
      matchMedia('(min-width: 1024px)').matches ? '.desktop-line' : '.mobile-line',
    )!
  line.onReach(0.12, () =>
    drawPath([
      ...visibleSvg().querySelectorAll<SVGPathElement>(
        '[data-browser-object] [data-object-stroke]',
      ),
    ]),
  )
  line.onReach(0.3, () =>
    drawPath([
      ...visibleSvg().querySelectorAll<SVGPathElement>('[data-diff-object] [data-object-stroke]'),
    ]),
  )
  line.onReach(0.39, () =>
    drawPath([...visibleSvg().querySelectorAll<SVGPathElement>('[data-dog-stroke]')]),
  )
  line.onReach(0.21, () =>
    gsap.fromTo(
      visibleSvg().querySelector('[data-click-rays]'),
      { opacity: 0 },
      { opacity: 1, duration: 0.25, ease: 'steps(2)', paused: true },
    ),
  )
  return () => line.destroy()
}
