import { plotterSchedule, scheduledProgress } from './progress'
import { gsap } from 'gsap'
import { ScrollTrigger } from 'gsap/ScrollTrigger'

gsap.registerPlugin(ScrollTrigger)
ScrollTrigger.config({ ignoreMobileResize: true, limitCallbacks: true })

export { gsap, ScrollTrigger }
export type ReachAnimation = gsap.core.Animation | (() => gsap.core.Animation)
export type LineFraction = number | { desktop: number; phone: number }
export interface LineOptions {
  start?: string
  end?: string
  /** Hero only: draw to the point marked data-intro-x/y before scroll takes over. */
  intro?: boolean
  introDuration?: number
  introStops?: LineFraction[]
  /** Optional source element for the hero line; measured only on refresh. */
  origin?: string
  /** Only Machine may pin, with a native scroll distance capped at 200vh. */
  pin?: boolean
  pinDistance?: number
}
export interface SectionLine {
  onReach(fraction: LineFraction, animation: ReachAnimation): SectionLine
  readonly timeline: gsap.core.Timeline | undefined
  destroy(): void
}
interface Reach {
  fraction: LineFraction
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
  let desktop = true
  const fractionOf = (fraction: LineFraction) =>
    typeof fraction === 'number' ? fraction : desktop ? fraction.desktop : fraction.phone
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
      desktop = !!context.conditions?.desktop
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
      const originalPath = path.getAttribute('d')!
      let points: Point[] = []
      let schedule: number[] = []
      let length = 0
      let dashScale = 1
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
      let intro: gsap.core.Tween | undefined
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
        if (!introDone && state.scroll > 0) {
          introDone = true
          intro?.kill()
          path.style.willChange = ''
        }
        currentProgress = options.intro
          ? introDone
            ? introEnd + (1 - introEnd) * state.scroll
            : state.intro
          : scheduledProgress(schedule, state.scroll)
        path.style.strokeDashoffset = String((1 - currentProgress) * dashScale)
        const penActive = currentProgress > 0 && currentProgress < 1
        if (pen) pen.style.opacity = penActive ? '1' : '0'
        const tip = pointAt(currentProgress)
        pen?.setAttribute('transform', `translate(${tip.x} ${tip.y})`)
        const spacing = (canvasWidth < 500 ? 9 : 11) / length
        dots.forEach((dot, i) => {
          const progress = currentProgress + spacing * (i + 1)
          const point = pointAt(progress)
          dot.setAttribute('transform', `translate(${point.x} ${point.y})`)
          dot.style.opacity = penActive && progress < 1 ? '1' : '0'
        })
        if (tick) {
          tick.style.opacity = currentProgress > 0 ? '1' : '0'
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
          const reached = currentProgress >= fractionOf(reach.fraction)
          if (reached === reach.reached) return
          reach.reached = reached
          const animation = makeAnimation(reach)
          if (reached) animation.play()
          else animation.reverse()
          if (!visible) animation.pause()
        })
      }
      const measure = () => {
        if (options.origin) {
          const source = section.querySelector(options.origin)
          const matrix = svg.getScreenCTM()
          const entry = originalPath.match(/^M([\d.]+)\s+([\d.]+)/)
          if (source && matrix && entry) {
            const rect = source.getBoundingClientRect()
            // The source sword's final grid row is empty: the visible tip is at 15/16.
            const point = new DOMPoint(
              rect.left + rect.width / 2,
              rect.top + (rect.height * 15) / 16,
            ).matrixTransform(matrix.inverse())
            const x = Number(entry[1])
            path.setAttribute(
              'd',
              originalPath.replace(
                /^M[\d.]+\s+[\d.]+/,
                `M${point.x} ${point.y} C${point.x} 104 ${x} 104 ${x} 140`,
              ),
            )
          }
        }
        length = path.getTotalLength()
        points = Array.from({ length: SAMPLES + 1 }, (_, i) => {
          const point = path.getPointAtLength((length * i) / SAMPLES)
          return { x: point.x, y: point.y }
        })
        // Non-scaling strokes also keep dash units in screen space. Compensate
        // pathLength normalization so the painted endpoint agrees with the pen.
        const matrix = path.getScreenCTM()
        if (matrix) {
          const sx = Math.hypot(matrix.a, matrix.b),
            sy = Math.hypot(matrix.c, matrix.d)
          if (Math.abs(sx - sy) < 0.0001) dashScale = sx
          else {
            let screenLength = 0,
              sampledLength = 0
            points.slice(1).forEach((point, i) => {
              const dx = point.x - points[i]!.x,
                dy = point.y - points[i]!.y
              sampledLength += Math.hypot(dx, dy)
              screenLength += Math.hypot(
                matrix.a * dx + matrix.c * dy,
                matrix.b * dx + matrix.d * dy,
              )
            })
            screenLength += ((length - sampledLength) * (sx + sy)) / 2
            dashScale = screenLength / length
          }
        }
        path.style.strokeDasharray = `${dashScale} ${dashScale}`
        const rect = section.getBoundingClientRect()
        sectionTop = rect.top + window.scrollY
        sectionHeight = rect.height
        const scaleY = matrix ? Math.hypot(matrix.c, matrix.d) : 1
        const sticky = desktop && section.id === 'machine'
        const reserved = sticky
          ? Math.max(0, rect.height - svg.getBoundingClientRect().height) / scaleY
          : 0
        schedule = plotterSchedule(points, reserved, window.innerHeight / scaleY)
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
        // Factory-created helpers cache geometry. Rebuild only at refresh and
        // preserve their playhead/direction so resizing never replays a section.
        reaches.forEach((reach) => {
          if (typeof reach.source !== 'function' || !reach.animation) return
          const progress = reach.animation.totalProgress()
          const paused = reach.animation.paused()
          const reversed = reach.animation.reversed()
          reach.animation.revert()
          reach.animation = undefined
          makeAnimation(reach)
            .totalProgress(progress)
            .reversed(reversed)
            .paused(paused || !visible)
        })
        render()
      }
      measure()
      section.classList.add('line-ready')
      timeline = gsap
        .timeline({ paused: true })
        .to(state, { scroll: 1, duration: 1, ease: 'none', onUpdate: render })
      const trigger = ScrollTrigger.create({
        trigger: section,
        animation: timeline,
        start: options.start ?? 'top 50%',
        end: options.pin
          ? `+=${Math.min(options.pinDistance ?? 150, 200)}%`
          : (options.end ?? 'bottom 50%'),
        scrub: 0.25,
        invalidateOnRefresh: true,
        pin: options.pin && section.id === 'machine',
      })
      intro =
        options.intro && !introDone
          ? gsap.to(state, {
              keyframes: [
                ...(options.introStops ?? []).map(fractionOf).filter((value) => value < introEnd),
                introEnd,
              ].map((value, _, stops) => ({
                intro: value,
                duration: (options.introDuration ?? 1.4) / stops.length,
                ease: 'expo.out',
              })),
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
        path.setAttribute('d', originalPath)
        pen?.style.removeProperty('opacity')
        tick?.style.removeProperty('opacity')
        currentProgress = 0
      }
    },
  )
  return {
    get timeline() {
      return timeline
    },
    onReach(fraction, source) {
      if (
        (typeof fraction === 'number' ? [fraction] : Object.values(fraction)).some(
          (value) => value < 0 || value > 1,
        )
      )
        throw new RangeError('Line fraction must be between 0 and 1')
      const reach: Reach = { fraction, source, reached: false }
      reaches.push(reach)
      if (reduced) makeAnimation(reach).progress(1).pause()
      else if (currentProgress >= fractionOf(fraction)) {
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
