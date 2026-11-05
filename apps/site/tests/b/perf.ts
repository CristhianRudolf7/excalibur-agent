import { productionBrowser } from '../build-browser.ts'
import { chromium } from '@playwright/test'
import { writeFile, mkdir } from 'node:fs/promises'
import { baseURL } from '../server.ts'
import { serveBuild } from './static.ts'

const stop = async () => {}
const browser = productionBrowser(await chromium.launch({ channel: 'chrome' }))
try {
  const page = await browser.newPage({ viewport: { width: 1536, height: 864 } })
  await serveBuild(page.context())
  const client = await page.context().newCDPSession(page)
  await client.send('Emulation.setCPUThrottlingRate', { rate: 4 })
  await page.addInitScript(() => {
    const results = {
      frames: [] as number[],
      tasks: [] as number[],
      taskDetails: [] as { start: number; duration: number }[],
      initialized: [] as { name: string; time: number }[],
      shifts: [] as unknown[],
      cls: 0,
      measuring: false,
    }
    Object.assign(window, { __perf: results })
    new MutationObserver((records) => {
      records.forEach((record) =>
        results.initialized.push({
          name: (record.target as HTMLElement).id,
          time: performance.now(),
        }),
      )
    }).observe(document, { subtree: true, attributes: true, attributeFilter: ['data-initialized'] })
    new PerformanceObserver((list) => {
      for (const entry of list.getEntries())
        if (results.measuring) {
          results.tasks.push(entry.duration)
          results.taskDetails.push({ start: entry.startTime, duration: entry.duration })
        }
    }).observe({ type: 'longtask', buffered: true })
    new PerformanceObserver((list) => {
      for (const entry of list.getEntries())
        if (!(entry as PerformanceEntry & { hadRecentInput: boolean }).hadRecentInput) {
          results.cls += (entry as PerformanceEntry & { value: number }).value
          results.shifts.push({
            ...entry.toJSON(),
            sources: (
              entry as PerformanceEntry & {
                sources: {
                  node?: Element
                  previousRect: DOMRectReadOnly
                  currentRect: DOMRectReadOnly
                }[]
              }
            ).sources.map((source) => ({
              node: source.node?.id || source.node?.className,
              previous: source.previousRect.toJSON(),
              current: source.currentRect.toJSON(),
            })),
          })
        }
    }).observe({ type: 'layout-shift', buffered: true })
  })
  await page.goto(baseURL)
  await page.evaluate(() => document.fonts.ready)
  await page.waitForTimeout(2500)
  const profileOrigin = await page.evaluate(() => performance.now())
  const profileEnabled = process.env.B_PROFILE === '1'
  if (profileEnabled) {
    await client.send('Profiler.enable')
    await client.send('Profiler.start')
  }
  const report = await page.evaluate(async () => {
    const data = (
      window as unknown as {
        __perf: {
          frames: number[]
          tasks: number[]
          taskDetails: { start: number; duration: number }[]
          initialized: { name: string; time: number }[]
          shifts: unknown[]
          cls: number
          measuring: boolean
        }
      }
    ).__perf
    data.measuring = true
    const distance = document.documentElement.scrollHeight - innerHeight
    const duration = 18000
    await new Promise<void>((resolve) => {
      let start = 0,
        previous = 0
      function frame(now: number) {
        if (!start) {
          start = now
          previous = now
        } else data.frames.push(now - previous)
        previous = now
        const progress = Math.min(1, (now - start) / duration)
        window.scrollTo(0, distance * progress)
        if (progress < 1) requestAnimationFrame(frame)
        else resolve()
      }
      requestAnimationFrame(frame)
    })
    await new Promise((resolve) => setTimeout(resolve, 350))
    data.measuring = false
    const sorted = data.frames.toSorted((a, b) => a - b)
    return {
      cpuThrottle: 4,
      frames: sorted.length,
      p50: sorted[Math.floor(sorted.length * 0.5)],
      p95: sorted[Math.floor(sorted.length * 0.95)],
      longestTask: Math.max(0, ...data.tasks),
      cls: data.cls,
      shifts: data.shifts,
      taskDetails: data.taskDetails,
      initialized: data.initialized,
      domNodes: document.querySelectorAll('*').length,
    }
  })
  if (profileEnabled) {
    const { profile } = await client.send('Profiler.stop')
    await writeFile(
      'test-results/b-scroll.cpuprofile',
      JSON.stringify({ ...profile, pageStart: profileOrigin }),
    )
  }
  const result = {
    ...report,
    directPreview: process.env.B_DIRECT_PREVIEW === '1',
    note: 'System Chrome, headless software rendering. Paint cost may exceed hardware rendering; frame intervals are rAF samples during an 18-second native scripted scroll of the local production landing build. The directPreview field identifies server versus request routing.',
  }
  console.log(JSON.stringify(result, null, 2))
  await mkdir('test-results', { recursive: true })
  await writeFile('test-results/b-perf.json', JSON.stringify(result, null, 2))
  if (report.longestTask > 100 || report.cls > 0.02) process.exitCode = 1
} finally {
  await browser.close()
  await stop()
}
