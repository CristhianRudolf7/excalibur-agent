import { prepareGeometry } from '../line/geometry'
import { createSectionLine, gsap } from '../line'

export function init(section: HTMLElement) {
  const restoreGeometry = prepareGeometry(section)
  const media = gsap.matchMedia()
  media.add(
    {
      desktop: '(min-width:1024px)',
      phone: '(max-width:1023px)',
      reduce: '(prefers-reduced-motion:reduce)',
    },
    (context) => {
      const line = createSectionLine(section, { end: 'bottom bottom' })
      const modules = [...section.querySelectorAll<HTMLElement>('[data-extension-module]')]
      if (context.conditions?.reduce) return () => line.destroy()
      const activate = (index: number) =>
        modules.forEach((module, i) => {
          module.querySelector('.b-plate')?.classList.toggle('b-active', i === index)
        })
      modules.forEach((module, i) => {
        line.onReach({ desktop: 0.28 + i * 0.13, phone: 0.2 + i * 0.13 }, () => {
          const desktop = !!context.conditions?.desktop
          const scale = section.getBoundingClientRect().width / 1536
          const distance = desktop ? Number(module.dataset.offset) * scale : 12
          const state = { value: desktop ? Number(module.dataset.offset) / 10 : 1.2 }
          const label = module.querySelector('[data-extension-z]')!
          let last = ''
          return gsap
            .timeline({
              paused: true,
              onStart: () => activate(i),
              onReverseComplete: () => activate(i > 0 ? i - 1 : 1),
            })
            .fromTo(module, { y: 0 }, { y: distance, duration: 0.55, ease: 'power3.inOut' }, 0)
            .to(
              state,
              {
                value: 0,
                duration: 0.55,
                ease: 'power3.inOut',
                onUpdate() {
                  const value = `z = ${state.value.toFixed(1)} mm`
                  if (last !== value) {
                    label.textContent = value
                    last = value
                  }
                },
              },
              0,
            )
        })
      })
      line.onReach(0.985, () =>
        gsap.fromTo(
          section.querySelectorAll('.extensions-sword rect[fill="var(--cyan)"]'),
          { opacity: 0 },
          { opacity: 1, duration: 0.3, ease: 'steps(2)', paused: true },
        ),
      )
      return () => {
        line.destroy()
        activate(1)
        modules.forEach((module) => {
          module.querySelector('[data-extension-z]')!.textContent =
            `z = ${(Number(module.dataset.offset) / 10).toFixed(1)} mm`
        })
      }
    },
  )
  return () => {
    media.revert()
    restoreGeometry()
  }
}
