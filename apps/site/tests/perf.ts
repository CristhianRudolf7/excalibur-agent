import { chromium } from '@playwright/test'
import { writeFile, mkdir } from 'node:fs/promises'
import { startPreview, baseURL } from './server.ts'

const stop = await startPreview()
const browser = await chromium.launch({ channel: 'chrome' })
try {
  const page = await browser.newPage({ viewport: { width: 1536, height: 864 } })
  const client = await page.context().newCDPSession(page)
  await client.send('Emulation.setCPUThrottlingRate', { rate: 4 })
  await page.addInitScript(() => {
    const results = { frames: [] as number[], tasks: [] as number[], cls: 0, measuring: false }
    Object.assign(window, { __perf: results })
    new PerformanceObserver((list) => {
      for (const entry of list.getEntries())
        if (results.measuring) results.tasks.push(entry.duration)
    }).observe({ type: 'longtask', buffered: true })
    new PerformanceObserver((list) => {
      for (const entry of list.getEntries())
        if (!(entry as PerformanceEntry & { hadRecentInput: boolean }).hadRecentInput)
          results.cls += (entry as PerformanceEntry & { value: number }).value
    }).observe({ type: 'layout-shift', buffered: true })
  })
  await page.goto(baseURL)
  await page.evaluate(() => document.fonts.ready)
  await page.waitForTimeout(2500)
  const report = await page.evaluate(async () => {
    const data = (
      window as unknown as {
        __perf: { frames: number[]; tasks: number[]; cls: number; measuring: boolean }
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
      domNodes: document.querySelectorAll('*').length,
    }
  })
  const result = {
    ...report,
    note: 'System Chrome, headless software rendering. Paint cost may exceed hardware rendering; frame intervals are rAF samples during a native scripted scroll of the current skeleton.',
  }
  console.log(JSON.stringify(result, null, 2))
  await mkdir('test-results', { recursive: true })
  await writeFile('test-results/perf.json', JSON.stringify(result, null, 2))
  if (report.longestTask > 100 || report.cls > 0.02) process.exitCode = 1
} finally {
  await browser.close()
  await stop()
}
