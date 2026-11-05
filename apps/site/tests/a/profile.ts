import { chromium } from '@playwright/test'
import { writeFile } from 'node:fs/promises'
import { baseURL } from '../server.ts'

const browser = await chromium.launch({ channel: 'chrome' })
try {
  const page = await browser.newPage({ viewport: { width: 1536, height: 864 } })
  const client = await page.context().newCDPSession(page)
  await client.send('Emulation.setCPUThrottlingRate', { rate: 4 })
  await page.addInitScript(() => {
    const tasks: unknown[] = []
    Object.assign(window, { __tasks: tasks })
    new PerformanceObserver((list) => {
      for (const task of list.getEntries())
        tasks.push({
          start: task.startTime,
          duration: task.duration,
          scroll: scrollY,
          sections: [...document.querySelectorAll('[data-initialized]')].map((el) => el.id),
        })
    }).observe({ type: 'longtask', buffered: true })
  })
  await page.goto(baseURL)
  await page.evaluate(() => document.fonts.ready)
  await page.waitForTimeout(2500)
  await client.send('Profiler.enable')
  await client.send('Profiler.start')
  await client.send('Tracing.start', {
    categories:
      'devtools.timeline,v8.execute,blink.user_timing,disabled-by-default-devtools.timeline',
    transferMode: 'ReturnAsStream',
  })
  await page.evaluate(async () => {
    const start = performance.now(),
      distance = document.documentElement.scrollHeight - innerHeight
    await new Promise<void>((resolve) => {
      function frame(now: number) {
        const progress = Math.min(1, (now - start) / 18000)
        scrollTo(0, distance * progress)
        if (progress < 1) requestAnimationFrame(frame)
        else resolve()
      }
      requestAnimationFrame(frame)
    })
  })
  const { profile } = await client.send('Profiler.stop')
  await writeFile('test-results/a-cpu.json', JSON.stringify(profile))
  console.log(
    JSON.stringify(
      profile.nodes
        .filter((node: any) => node.hitCount > 5)
        .sort((a: any, b: any) => b.hitCount - a.hitCount)
        .slice(0, 20),
      null,
      2,
    ),
  )
  const complete = new Promise<any>((resolve) => client.once('Tracing.tracingComplete', resolve))
  await client.send('Tracing.end')
  const { stream } = await complete
  let source = ''
  for (;;) {
    const part = await client.send('IO.read', { handle: stream })
    source += part.data
    if (part.eof) break
  }
  await client.send('IO.close', { handle: stream })
  await writeFile('test-results/a-trace.json', source)
  const tasks = await page.evaluate(() => (window as unknown as { __tasks: unknown[] }).__tasks)
  console.log(JSON.stringify(tasks, null, 2))
  await writeFile('test-results/a-tasks.json', JSON.stringify(tasks, null, 2))
  const events = JSON.parse(source).traceEvents as Array<{
    name: string
    dur?: number
    ts: number
    args?: unknown
  }>
  console.log(
    JSON.stringify(
      events
        .filter((e) => (e.dur ?? 0) > 50000)
        .sort((a, b) => (b.dur ?? 0) - (a.dur ?? 0))
        .slice(0, 35),
      null,
      2,
    ),
  )
} finally {
  await browser.close()
}
