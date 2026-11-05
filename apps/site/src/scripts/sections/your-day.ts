import { prepareGeometry } from '../line/geometry'
import { createSectionLine, gsap } from '../line'

export function init(section: HTMLElement) {
  const restoreGeometry = prepareGeometry(section)
  const line = createSectionLine(section)
  line.onReach({ desktop: 0.18, phone: 0.27 }, () =>
    gsap.fromTo(
      section.querySelectorAll('[data-wave]'),
      { scaleY: 0.08 },
      { scaleY: 1, duration: 0.35, stagger: 0.012, ease: 'expo.out', paused: true },
    ),
  )
  line.onReach({ desktop: 0.25, phone: 0.36 }, () =>
    gsap.fromTo(
      section.querySelectorAll('[data-word]'),
      { opacity: 0 },
      { opacity: 1, duration: 0.05, stagger: 0.055, ease: 'steps(1)', paused: true },
    ),
  )
  line.onReach({ desktop: 0.51, phone: 0.53 }, () => {
    const highlight = section.querySelector<HTMLElement>('.day-action-highlight')!
    const targets = [...section.querySelectorAll<HTMLElement>('[data-action-target]')]
    const origin = highlight.getBoundingClientRect()
    const positions = targets.map((target) => {
      const rect = target.getBoundingClientRect()
      return {
        x: rect.left - origin.left,
        y: rect.top - origin.top,
        scaleX: rect.width / origin.width,
        scaleY: rect.height / origin.height,
      }
    })
    return gsap
      .timeline({ paused: true })
      .fromTo(
        highlight,
        { ...positions[0], opacity: 0 },
        { ...positions[0], opacity: 1, duration: 0.25, ease: 'steps(1)' },
      )
      .to(highlight, { ...positions[1], duration: 0.25, ease: 'steps(1)' })
      .fromTo(
        section.querySelectorAll('[data-password-dot]'),
        { opacity: 0 },
        { opacity: 1, stagger: 0.035, duration: 0.05, ease: 'steps(1)' },
      )
      .to(highlight, { x: 0, y: 0, scaleX: 1, scaleY: 1, duration: 0.25, ease: 'steps(1)' })
  })
  line.onReach({ desktop: 0.75, phone: 0.72 }, () =>
    gsap
      .timeline({ paused: true })
      .fromTo(
        section.querySelector('[data-cursor]'),
        { x: 24, y: 24, opacity: 0 },
        { x: 0, y: 0, opacity: 1, duration: 0.4, ease: 'power3.inOut' },
      )
      .fromTo(
        section.querySelector('[data-burst]'),
        { scale: 0.5, opacity: 0, transformOrigin: '50% 80%' },
        { scale: 1, opacity: 1, duration: 0.25, ease: 'expo.out' },
      )
      .fromTo(
        section.querySelector('[data-click-tag]'),
        { opacity: 0 },
        { opacity: 1, duration: 0.25 },
      ),
  )
  return () => {
    line.destroy()
    restoreGeometry()
  }
}
