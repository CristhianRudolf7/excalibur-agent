import { prepareGeometry } from '../line/geometry'
import { createSectionLine, gsap } from '../line'

export function init(section: HTMLElement) {
  const restoreGeometry = prepareGeometry(section)
  const line = createSectionLine(section)
  const documents = [...section.querySelectorAll<HTMLElement>('[data-work-document]')]
  const stops = [0, 256, 490, 686, 956, 1226]
  documents.forEach((document, i) => {
    line.onReach({ desktop: 0.31 + i * 0.055, phone: 0.16 + i * 0.115 }, () => {
      const tl = gsap
        .timeline({ paused: true })
        .fromTo(
          document,
          { opacity: 0, scaleY: 0.92 },
          { opacity: 1, scaleY: 1, duration: 0.35, ease: 'expo.out' },
        )
      const desktop = matchMedia('(min-width:1024px)').matches
      const svg = section.querySelector<SVGSVGElement>(desktop ? '.desktop-line' : '.mobile-line')!
      const train = svg.querySelector('[data-work-train]')
      if (train && desktop)
        tl.fromTo(
          train,
          { x: stops[Math.max(0, i - 1)]! },
          { x: stops[i]!, duration: 0.45, ease: 'power3.inOut' },
          0,
        )
      if (train && !desktop) {
        const inverse = svg.getScreenCTM()!.inverse()
        const positions = [...section.querySelectorAll<HTMLElement>('.work-station')].map(
          (station) => {
            const rect = station.getBoundingClientRect()
            return new DOMPoint(rect.left, rect.top + 30).matrixTransform(inverse).y - 298
          },
        )
        tl.fromTo(
          train,
          { y: positions[Math.max(0, i - 1)]! },
          { y: positions[i]!, duration: 0.45, ease: 'power3.inOut' },
          0,
        )
      }
      return tl
    })
  })
  line.onReach(0.72, () =>
    gsap.fromTo(
      section.querySelectorAll('[data-work-train]'),
      { opacity: 1 },
      { opacity: 0, duration: 0.25, paused: true },
    ),
  )
  line.onReach({ desktop: 0.74, phone: 0.86 }, () =>
    gsap.fromTo(
      section.querySelector('[data-work-digits]'),
      { yPercent: -100 },
      { yPercent: 0, duration: 0.45, ease: 'steps(5)', paused: true },
    ),
  )
  return () => {
    line.destroy()
    restoreGeometry()
  }
}
