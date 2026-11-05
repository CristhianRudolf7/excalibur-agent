import { chromium } from '@playwright/test'
import { mkdir, writeFile, readdir } from 'node:fs/promises'
import { resolve } from 'node:path'
import { spawn } from 'node:child_process'
import { productionBrowser } from '../build-browser.ts'
import { baseURL, startPreview } from '../server.ts'

const out = resolve(
  process.env.QA_OUT ?? '/home/cristhian/projects/ui-conceptos/landing/implementacion/final',
)
const sections = [
  'top',
  'your-day',
  'memory',
  'data',
  'code',
  'work',
  'machine',
  'measured',
  'install',
  'extensions',
]
const widths = (process.env.QA_WIDTHS ?? '1280,1536,1920,390').split(',').map(Number)
const stop = await startPreview()
const browser = productionBrowser(await chromium.launch({ channel: 'chrome' }))
const errors: string[] = []
const report: Record<string, unknown> = {
  origin: baseURL,
  source: 'This checkout: production dist',
  captures: [],
  errors,
}
await mkdir(out, { recursive: true })
try {
  for (const theme of ['light', 'dark'] as const) {
    for (const width of widths) {
      const page = await browser.newPage({
        viewport: { width, height: width < 1024 ? 844 : (width * 9) / 16 },
        colorScheme: theme,
        reducedMotion: 'reduce',
      })
      page.on('pageerror', (error) => errors.push(error.message))
      await page.goto(baseURL)
      await page.evaluate(() => document.fonts.ready)
      for (const name of sections) {
        const section = page.locator(`#${name}`)
        await section.scrollIntoViewIfNeeded()
        await page.waitForTimeout(100)
        await section.screenshot({
          path: `${out}/${name === 'top' ? 'hero' : name}-${width}-${theme}.png`,
        })
        await page.screenshot({
          path: `${out}/${name === 'top' ? 'hero' : name}-${width}-${theme}-viewport.png`,
        })
      }
      if (await page.evaluate(() => document.documentElement.scrollWidth > innerWidth))
        errors.push(`Overflow: ${width}/${theme}`)
      await page.close()
      const moving = await browser.newPage({
        viewport: { width, height: width < 1024 ? 844 : (width * 9) / 16 },
        colorScheme: theme,
      })
      moving.on('pageerror', (error) => errors.push(error.message))
      await moving.goto(baseURL)
      await moving.evaluate(() => document.fonts.ready)
      for (const name of sections) {
        await moving
          .locator(`#${name}`)
          .evaluate((el) =>
            scrollTo(0, el.getBoundingClientRect().top + scrollY - innerHeight * 0.1),
          )
        await moving.waitForTimeout(330)
        await moving.screenshot({
          path: `${out}/${name === 'top' ? 'hero' : name}-${width}-${theme}-mid.png`,
        })
      }
      await moving.close()
    }
    for (const width of [1536, 390]) {
      const page = await browser.newPage({
        viewport: { width, height: width < 1024 ? 844 : 864 },
        colorScheme: theme,
        reducedMotion: 'reduce',
      })
      for (const [locale, route] of [
        ['en', '/docs/'],
        ['es', '/es/docs/'],
      ]) {
        await page.goto(`${baseURL}${route}`)
        await page.evaluate(() => document.fonts.ready)
        await page.screenshot({
          path: `${out}/docs-home-${locale}-${width}-${theme}.png`,
          fullPage: true,
        })
        await page.goto(`${baseURL}${route}getting-started/install/`)
        await page.screenshot({
          path: `${out}/docs-install-${locale}-${width}-${theme}.png`,
          fullPage: true,
        })
        await page
          .getByRole('button', { name: /Search|Buscar/ })
          .first()
          .click()
        await page.locator('#starlight__search input').fill(locale === 'es' ? 'memoria' : 'memory')
        await page.waitForTimeout(700)
        await page.screenshot({ path: `${out}/docs-search-${locale}-${width}-${theme}.png` })
      }
      await page.close()
      const noJS = await browser.newPage({
        viewport: { width, height: 844 },
        colorScheme: theme,
        javaScriptEnabled: false,
      })
      await noJS.goto(baseURL)
      await noJS.evaluate(() => document.fonts.ready)
      await noJS
        .locator('#install')
        .screenshot({ path: `${out}/install-${width}-${theme}-no-js.png` })
      await noJS.close()
    }
    if (process.env.QA_VIDEO === '1') {
      const context = await browser.newContext({
        viewport: { width: 1536, height: 864 },
        colorScheme: theme,
        recordVideo: { dir: `${out}/video-source`, size: { width: 1536, height: 864 } },
      })
      const page = await context.newPage()
      await page.goto(baseURL)
      await page.evaluate(() => document.fonts.ready)
      await page.waitForTimeout(1800)
      await page.evaluate(async () => {
        const distance = document.documentElement.scrollHeight - innerHeight
        await new Promise<void>((resolve) => {
          let start = 0
          function frame(now: number) {
            start ||= now
            const progress = Math.min(1, (now - start) / 40000)
            scrollTo(0, distance * progress)
            progress < 1 ? requestAnimationFrame(frame) : resolve()
          }
          requestAnimationFrame(frame)
        })
      })
      await page.waitForTimeout(1000)
      const video = page.video()!
      await context.close()
      const source = await video.path()
      await new Promise<void>((resolve, reject) => {
        const child = spawn(
          'nice',
          [
            '-n',
            '10',
            'ffmpeg',
            '-y',
            '-i',
            source,
            '-c:v',
            'libx264',
            '-preset',
            'fast',
            '-crf',
            '23',
            '-pix_fmt',
            'yuv420p',
            '-movflags',
            '+faststart',
            `${out}/scroll-${theme}.mp4`,
          ],
          { stdio: 'ignore' },
        )
        child.once('exit', (code) => (code === 0 ? resolve() : reject(new Error(`ffmpeg ${code}`))))
      })
    }
  }
} finally {
  report.captures = (await readdir(out)).filter((name) => name.endsWith('.png')).sort()
  report.videos = (await readdir(out)).filter((name) => name.endsWith('.mp4')).sort()
  await writeFile(`${out}/capture-report.json`, JSON.stringify(report, null, 2))
  await browser.close()
  await stop()
}
if (errors.length) throw new Error(errors.join('\n'))
console.log(`Final audit screenshots: ${out}`)
