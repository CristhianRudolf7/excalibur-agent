import { gsap } from 'gsap'
import { ScrollTrigger } from 'gsap/ScrollTrigger'

gsap.registerPlugin(ScrollTrigger)
ScrollTrigger.config({ ignoreMobileResize: true, limitCallbacks: true })

export { gsap, ScrollTrigger }
export type ReachAnimation = gsap.core.Animation | (() => gsap.core.Animation)
export interface LineOptions {
  start?: string
  end?: string
  /** Hero only: draw to the point marked data-intro-x/y before scroll takes over. */
  intro?: boolean
  introDuration?: number
  /** Only Machine may pin, with a native scroll distance capped at 200vh. */
  pin?: boolean
  pinDistance?: number
}
export interface SectionLine {
  onReach(fraction: number, animation: ReachAnimation): SectionLine
  readonly timeline: gsap.core.Timeline | undefined
  destroy(): void
}
interface Reach {
  fraction: number
  source: ReachAnimation
  animation?: gsap.core.Animation
  reached: boolean
}
interface Point {
  x: number
  y: number
}
const SAMPLES = 360
const PAGE_SECONDS = 37.7

/** A section owns one main path in each responsive SVG. Reads occur only at setup/refresh. */
export function createSectionLine(section: HTMLElement, options: LineOptions = {}): SectionLine {
  const media = gsap.matchMedia()
  const reaches: Reach[] = []
  let timeline: gsap.core.Timeline | undefined
  let reduced = false
  let currentProgress = 0
  let visible = true
  const makeAnimation = (reach: Reach) => {
    if (!reach.animation) {
      reach.animation = typeof reach.source === 'function' ? reach.source() : reach.source
      reach.animation.pause(0)
    }
    return reach.animation
  }
  media.add(
    {
      desktop: '(min-width: 1024px)',
      phone: '(max-width: 1023px)',
      reduce: '(prefers-reduced-motion: reduce)',
    },
    (context) => {
      reduced = !!context.conditions?.reduce
      const selector = context.conditions?.desktop ? '.desktop-line' : '.mobile-line'
      const svg = section.querySelector<SVGSVGElement>(selector)
      const path = svg?.querySelector<SVGPathElement>('[data-line-path]')
      if (!svg || !path) return
      if (reduced) {
        path.style.strokeDashoffset = '0'
        section.classList.remove('line-ready')
        section
          .querySelectorAll<HTMLElement>('[data-reveal]')
          .forEach((el) => (el.style.opacity = '1'))
        reaches.forEach((reach) => makeAnimation(reach).progress(1).pause())
        return () => {
          path.style.removeProperty('stroke-dashoffset')
        }
      }
      let points: Point[] = []
      let length = 0
      let sectionTop = 0,
        sectionHeight = 1,
        pageHeight = 1,
        canvasWidth = 1536
      let introEnd = 0
      let tenth = -1
      let introDone = !options.intro
      const pen = svg.querySelector<SVGCircleElement>('[data-pen]')
      const dots = [...svg.querySelectorAll<SVGCircleElement>('[data-preview]')]
      const tick = svg.querySelector<SVGGElement>('[data-time-tick]')
      const label = tick?.querySelector<SVGTextElement>('[data-time-label]')
      const state = { scroll: 0, intro: 0 }
      const pointAt = (fraction: number): Point => {
        const index = Math.min(SAMPLES, Math.max(0, fraction * SAMPLES))
        const low = Math.floor(index),
          high = Math.min(SAMPLES, low + 1),
          t = index - low
        return {
          x: points[low]!.x + (points[high]!.x - points[low]!.x) * t,
          y: points[low]!.y + (points[high]!.y - points[low]!.y) * t,
        }
      }
      const render = () => {
        if (!points.length) return
        currentProgress = introDone ? introEnd + (1 - introEnd) * state.scroll : state.intro
        path.style.strokeDashoffset = String(1 - currentProgress)
        const tip = pointAt(currentProgress)
        pen?.setAttribute('transform', `translate(${tip.x} ${tip.y})`)
        const spacing = (canvasWidth < 500 ? 9 : 11) / length
        dots.forEach((dot, i) => {
          const progress = currentProgress + spacing * (i + 1)
          const point = pointAt(progress)
          dot.setAttribute('transform', `translate(${point.x} ${point.y})`)
          dot.style.opacity = progress < 1 ? '1' : '0'
        })
        if (tick) {
          const location = pointAt(
            Math.max(0, currentProgress - (canvasWidth < 500 ? 58 : 500) / length),
          )
          tick.setAttribute(
            'transform',
            `translate(${Math.min(canvasWidth - 54, Math.max(54, location.x))} ${location.y})`,
          )
          const seconds =
            ((sectionTop + currentProgress * sectionHeight) / pageHeight) * PAGE_SECONDS
          const nextTenth = Math.floor(seconds * 10)
          if (label && nextTenth !== tenth) {
            tenth = nextTenth
            label.textContent = `00:${(tenth / 10).toFixed(1).padStart(4, '0')}`
          }
        }
        reaches.forEach((reach) => {
          const reached = currentProgress >= reach.fraction
          if (reached === reach.reached) return
          reach.reached = reached
          const animation = makeAnimation(reach)
          if (reached) animation.play()
          else animation.reverse()
          if (!visible) animation.pause()
        })
      }
      const measure = () => {
        length = path.getTotalLength()
        points = Array.from({ length: SAMPLES + 1 }, (_, i) => {
          const point = path.getPointAtLength((length * i) / SAMPLES)
          return { x: point.x, y: point.y }
        })
        const rect = section.getBoundingClientRect()
        sectionTop = rect.top + window.scrollY
        sectionHeight = rect.height
        pageHeight = Math.max(document.documentElement.scrollHeight, 1)
        canvasWidth = svg.viewBox.baseVal.width
        if (options.intro) {
          const target = { x: Number(path.dataset.introX), y: Number(path.dataset.introY) }
          let closest = 0,
            distance = Infinity
          points.forEach((point, i) => {
            const next = (point.x - target.x) ** 2 + (point.y - target.y) ** 2
            if (next < distance) {
              closest = i
              distance = next
            }
          })
          introEnd = closest / SAMPLES
        }
        render()
      }
      measure()
      path.style.strokeDasharray = '1 1'
      section.classList.add('line-ready')
      timeline = gsap
        .timeline({ paused: true })
        .to(state, { scroll: 1, duration: 1, ease: 'none', onUpdate: render })
      const trigger = ScrollTrigger.create({
        trigger: section,
        animation: timeline,
        start: options.start ?? 'top 62%',
        end: options.pin
          ? `+=${Math.min(options.pinDistance ?? 150, 200)}%`
          : (options.end ?? 'bottom 62%'),
        scrub: 0.25,
        invalidateOnRefresh: true,
        pin: options.pin && section.id === 'machine',
      })
      const intro = options.intro
        ? gsap.to(state, {
            intro: introEnd,
            duration: options.introDuration ?? 1.4,
            ease: 'expo.inOut',
            onUpdate: render,
            onComplete: () => {
              introDone = true
              render()
              path.style.willChange = ''
            },
          })
        : undefined
      if (intro) path.style.willChange = 'stroke-dashoffset'
      const observer = new IntersectionObserver(([entry]) => {
        visible = entry?.isIntersecting ?? false
        if (!visible) {
          intro?.pause()
          trigger.getTween()?.pause()
          reaches.forEach((reach) => reach.animation?.pause())
        } else {
          if (!introDone) intro?.resume()
          reaches.forEach((reach) => {
            if (reach.animation) {
              if (reach.reached) reach.animation.play()
              else reach.animation.reverse()
            }
          })
        }
      })
      observer.observe(section)
      ScrollTrigger.addEventListener('refresh', measure)
      return () => {
        observer.disconnect()
        ScrollTrigger.removeEventListener('refresh', measure)
        trigger.kill()
        intro?.kill()
        timeline?.kill()
        reaches.forEach((reach) => {
          reach.animation?.revert()
          reach.animation = undefined
          reach.reached = false
        })
        section.classList.remove('line-ready')
        path.style.removeProperty('stroke-dashoffset')
        path.style.removeProperty('stroke-dasharray')
        path.style.removeProperty('will-change')
        currentProgress = 0
      }
    },
  )
  return {
    get timeline() {
      return timeline
    },
    onReach(fraction, source) {
      if (fraction < 0 || fraction > 1)
        throw new RangeError('Line fraction must be between 0 and 1')
      const reach: Reach = { fraction, source, reached: false }
      reaches.push(reach)
      if (reduced) makeAnimation(reach).progress(1).pause()
      else if (currentProgress >= fraction) {
        reach.reached = true
        makeAnimation(reach).play()
      }
      return this
    },
    destroy() {
      media.revert()
    },
  }
}
