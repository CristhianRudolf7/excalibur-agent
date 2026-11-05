import { createSectionLine, gsap } from '../line'

export function init(section: HTMLElement) {
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
      const train = section.querySelector('[data-work-train]')
      if (train && matchMedia('(min-width:1024px)').matches)
        tl.fromTo(
          train,
          { x: stops[Math.max(0, i - 1)]! },
          { x: stops[i]!, duration: 0.45, ease: 'power3.inOut' },
          0,
        )
      return tl
    })
  })
  line.onReach(0.72, () =>
    gsap.fromTo(
      section.querySelector('[data-work-train]'),
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
  return () => line.destroy()
}
