import { prepareGeometry } from '../line/geometry'
import { createSectionLine, gsap } from '../line'
import { drawPath, reveal } from '../line/helpers'

export function init(section: HTMLElement) {
  const restoreGeometry = prepareGeometry(section)
  const line = createSectionLine(section)
  const branches = [...section.querySelectorAll<SVGPathElement>('[data-code-branch]')]
  branches.forEach((path, i) =>
    line.onReach(0.18 + i * 0.026, () => {
      const tween = drawPath(path)
      tween.vars.ease = 'none'
      return tween.invalidate()
    }),
  )
  line.onReach(0.19, () => reveal(section.querySelectorAll('[data-code-agent]')))
  line.onReach(0.28, () => reveal(section.querySelectorAll('[data-code-sub]')))
  line.onReach(0.37, () => reveal(section.querySelector('[data-code-diff]')))
  line.onReach(0.45, () =>
    gsap
      .timeline({ paused: true })
      .fromTo(
        section.querySelectorAll('[data-code-check]'),
        { opacity: 0 },
        { opacity: 1, stagger: 0.13, duration: 0.1, ease: 'steps(1)' },
      )
      .fromTo(
        section.querySelector('.code-test-failed'),
        { opacity: 1 },
        { opacity: 0, duration: 0.25, ease: 'steps(1)' },
        0.25,
      )
      .fromTo(
        section.querySelector('[data-test-result]'),
        { opacity: 0 },
        { opacity: 1, duration: 0.25, ease: 'steps(1)' },
        0.25,
      ),
  )
  line.onReach(0.72, () => reveal(section.querySelector('[data-code-preview]')))
  return () => {
    line.destroy()
    restoreGeometry()
  }
}
