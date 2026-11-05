import { prepareGeometry } from '../line/geometry'
import { createSectionLine, gsap } from '../line'

export function init(section: HTMLElement) {
  const restoreGeometry = prepareGeometry(section)
  const line = createSectionLine(section)
  const media = gsap.matchMedia()
  media.add('(prefers-reduced-motion:no-preference)', () => {
    if (!line.timeline) return
    const number = section.querySelector<HTMLElement>('[data-measured-number]')!
    const state = { value: 0 }
    let last = ''
    line.timeline.to(
      state,
      {
        value: 59.6,
        duration: 0.32,
        ease: 'none',
        onUpdate() {
          const value = state.value.toFixed(1)
          if (value !== last) {
            number.textContent = value
            last = value
          }
        },
      },
      0.32,
    )
    return () => {
      number.textContent = '59.6'
    }
  })
  section.querySelectorAll('[data-measured-tick]').forEach((tick, i) => {
    line.onReach({ desktop: 0.58 + i * 0.055, phone: 0.4 + i * 0.05 }, () =>
      gsap.fromTo(
        tick,
        { opacity: 0, y: 8 },
        { opacity: 1, y: 0, duration: 0.3, ease: 'expo.out', paused: true },
      ),
    )
  })
  return () => {
    media.revert()
    line.destroy()
    restoreGeometry()
  }
}
