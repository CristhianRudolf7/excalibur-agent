import { prepareGeometry } from '../line/geometry'
import { createSectionLine, gsap, ScrollTrigger } from '../line'
import { reveal } from '../line/helpers'

/** One constant converts actual translated drawing coordinates to decorative mm. */
const UNITS_PER_MM = 10
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
      const desktop = !!context.conditions?.desktop
      // A native sticky canvas reserves the 150vh journey before lazy init.
      // It also avoids the shared pin adapter's synchronous global refresh.
      const line = createSectionLine(section, {
        start: desktop ? 'top top' : 'top 62%',
        end: desktop ? '+=150%' : 'bottom 62%',
      })
      const layers = [...section.querySelectorAll<HTMLElement>('[data-machine-layer]')]
      if (!desktop) {
        layers.forEach((layer, i) => line.onReach(0.2 + i * 0.11, () => reveal(layer)))
        return () => line.destroy()
      }
      if (context.conditions?.reduce || !line.timeline) return () => line.destroy()
      layers[2]?.querySelector('.b-plate')?.classList.remove('b-active')
      const origins = layers.map((layer) => Number(layer.dataset.origin))
      const shifts = layers.map((layer) => Number(layer.dataset.shift))
      const counters = layers.map((layer) => layer.querySelector('[data-machine-z]')!)
      const dimensions = [...section.querySelectorAll<HTMLElement>('[data-machine-dimension]')]
      const packet = section.querySelector<HTMLElement>('[data-machine-packet]')!
      const clock = section.querySelector('[data-machine-clock]')!
      const layerY = layers.map((layer) => gsap.quickSetter(layer, 'y', 'px'))
      const dimensionSetters = dimensions.map((dimension) => ({
        y: gsap.quickSetter(dimension, 'y', 'px'),
        rule: gsap.quickSetter(dimension.querySelector('.machine-dimension-rule'), 'scaleY'),
        cap: gsap.quickSetter(dimension.querySelector('.machine-dimension-cap'), 'y', 'px'),
        label: gsap.quickSetter(dimension.querySelector('span'), 'y', 'px'),
        text: dimension.querySelector('[data-machine-distance]')!,
      }))
      const packetY = gsap.quickSetter(packet, 'y', 'px')
      const packetOpacity = gsap.quickSetter(packet, 'opacity')
      let active = -1
      const state = { progress: 0 }
      let scale = section.getBoundingClientRect().width / 1536
      const measure = () => {
        scale = section.getBoundingClientRect().width / 1536
      }
      ScrollTrigger.addEventListener('refresh', measure)
      const last = new Map<Element, string>()
      const text = (element: Element, value: string) => {
        if (last.get(element) !== value) {
          element.textContent = value
          last.set(element, value)
        }
      }
      const render = () => {
        const p = state.progress
        const positions = origins.map((origin, i) => origin + shifts[i]! * p)
        layers.forEach((_, i) => {
          layerY[i]!(shifts[i]! * p * scale)
          text(
            counters[i]!,
            `z = ${((positions[i]! - positions[0]!) / UNITS_PER_MM).toFixed(1)} mm`,
          )
        })
        const nextActive = Math.min(5, Math.floor(p * 6))
        if (nextActive !== active) {
          active = nextActive
          layers.forEach((layer, i) => layer.classList.toggle('b-seated', i !== 4 && i === active))
        }
        dimensionSetters.forEach((set, i) => {
          const gap = positions[i + 1]! - positions[i]!,
            baseGap = origins[i + 1]! - origins[i]!
          set.y(shifts[i]! * p * scale)
          set.rule(gap / baseGap)
          set.cap((gap - baseGap) * scale)
          set.label(((gap - baseGap) * scale) / 2)
          text(set.text, `${(gap / UNITS_PER_MM).toFixed(1)} mm`)
        })
        const segment = Math.min(4, Math.floor(p * 5))
        const phase = p === 1 ? 1 : (p * 5) % 1
        packetY(
          (positions[segment]! + (positions[segment + 1]! - positions[segment]!) * phase - 250) *
            scale,
        )
        packetOpacity(p === 1 ? 0 : 1)
        text(clock, `00:${(p * 2.2).toFixed(1).padStart(4, '0')}`)
      }
      line.timeline
        .to(state, { progress: 1, duration: 1, ease: 'power3.inOut', onUpdate: render }, 0)
        .fromTo(
          section.querySelector('[data-machine-progress]'),
          { scaleX: 0 },
          { scaleX: 1, duration: 1, ease: 'none' },
          0,
        )
      return () => {
        ScrollTrigger.removeEventListener('refresh', measure)
        line.destroy()
        layers[2]?.querySelector('.b-plate')?.classList.add('b-active')
        layers.forEach((layer) => {
          layer.classList.remove('b-seated')
          gsap.set(layer, { clearProps: 'transform' })
        })
        gsap.set(
          [
            ...dimensions,
            ...section.querySelectorAll(
              '.machine-dimension-rule,.machine-dimension-cap,.machine-dimension span',
            ),
            packet,
          ],
          { clearProps: 'transform,opacity' },
        )
        dimensions.forEach((dimension, i) => {
          dimension.querySelector('[data-machine-distance]')!.textContent =
            `${((origins[i + 1]! - origins[i]!) / UNITS_PER_MM).toFixed(1)} mm`
        })
        clock.textContent = '00:00.0'
        counters.forEach((counter, i) => {
          counter.textContent = `z = ${((origins[i]! - origins[0]!) / UNITS_PER_MM).toFixed(1)} mm`
        })
      }
    },
  )
  return () => {
    media.revert()
    restoreGeometry()
  }
}
