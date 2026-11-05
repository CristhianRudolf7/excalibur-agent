import { createSectionLine, gsap } from '../line'
import { drawPath } from '../line/helpers'

export function init(section: HTMLElement) {
  const line = createSectionLine(section, {
    intro: true,
    origin: '.wordmark-sword',
    start: 'top top',
    introStops: [
      { desktop: 0.216, phone: 0.236 },
      { desktop: 0.322, phone: 0.333 },
      { desktop: 0.408, phone: 0.422 },
    ],
  })
  const visibleSvg = () =>
    section.querySelector<SVGSVGElement>(
      matchMedia('(min-width: 1024px)').matches ? '.desktop-line' : '.mobile-line',
    )!
  const drawObject = (selector: string) => {
    const group = visibleSvg().querySelector<SVGGElement>(selector)!
    const paths = [
      ...group.querySelectorAll<SVGPathElement>('[data-object-stroke], [data-dog-stroke]'),
    ]
    return gsap
      .timeline({ paused: true })
      .fromTo(group, { opacity: 0 }, { opacity: 1, duration: 0.12, ease: 'power3.inOut' }, 0)
      .add(drawPath(paths).duration(0.35).paused(false), 0)
  }
  line.onReach({ desktop: 0.2, phone: 0.236 }, () => drawObject('[data-browser-object]'))
  line.onReach({ desktop: 0.316, phone: 0.333 }, () => drawObject('[data-diff-object]'))
  line.onReach({ desktop: 0.405, phone: 0.422 }, () => drawObject('.quadruped'))
  line.onReach({ desktop: 0.3, phone: 0.333 }, () => {
    const rays = visibleSvg().querySelector('[data-click-rays]')
    return rays
      ? gsap.fromTo(
          rays,
          { opacity: 0 },
          { opacity: 1, duration: 0.25, ease: 'steps(2)', paused: true },
        )
      : gsap.timeline({ paused: true })
  })
  return () => line.destroy()
}
