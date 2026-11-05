import { productionBrowser } from './build-browser.ts'
import { chromium } from '@playwright/test'
import { mkdir, writeFile } from 'node:fs/promises'
import { resolve } from 'node:path'
import { startPreview, baseURL } from './server.ts'

const args = process.argv.slice(2)
const option = (key: string, fallback: string) => {
  const i = args.indexOf(`--${key}`)
  return i < 0 ? fallback : args[i + 1]!
}
const name = option('section', 'hero')
const noJS = args.includes('--no-js')
const motion = option('motion', name === 'hero' ? 'no-preference' : 'reduce') as
  'reduce' | 'no-preference'
const variant = noJS ? '-no-js' : motion === 'reduce' && name === 'hero' ? '-reduced' : ''
const anchor = name === 'hero' ? 'top' : name
const out = resolve(option('out', 'test-results/shots'))
const widths = option('widths', '1280,1536,1920,390').split(',').map(Number)
const themes = option('theme', 'light,dark').split(',') as ('light' | 'dark')[]
const stop = await startPreview()
const browser = productionBrowser(await chromium.launch({ channel: 'chrome' }))
await mkdir(out, { recursive: true })
try {
  for (const theme of themes)
    for (const width of widths) {
      const page = await browser.newPage({
        viewport: { width, height: width === 390 ? 844 : Math.round((width * 9) / 16) },
        colorScheme: theme,
        reducedMotion: motion,
        javaScriptEnabled: !noJS,
      })
      await page.goto(`${baseURL}/#${anchor}`)
      await page.evaluate(() => document.fonts.ready)
      await page.waitForTimeout(2200)
      const section = page.locator(`#${anchor}`)
      await page.screenshot({ path: `${out}/${name}-${width}-${theme}${variant}-viewport.png` })
      const clip = await section.evaluate((el) => {
        const rect = el.getBoundingClientRect()
        return {
          x: rect.left + scrollX,
          y: rect.top + scrollY,
          width: rect.width,
          height: rect.height,
          scale: 1,
        }
      })
      // Capture beyond the viewport without Playwright scrolling/resizing the
      // element into view: those actions would advance a scroll-driven drawing.
      const client = await page.context().newCDPSession(page)
      const capture = await client.send('Page.captureScreenshot', {
        format: 'png',
        clip,
        captureBeyondViewport: true,
      })
      await writeFile(
        `${out}/${name}-${width}-${theme}${variant}.png`,
        Buffer.from(capture.data, 'base64'),
      )
      await page.close()
    }
  if (name === 'hero' && !noJS && motion === 'no-preference') {
    const page = await browser.newPage({
      viewport: { width: 1536, height: 864 },
      colorScheme: 'light',
    })
    await page.goto(baseURL)
    await page.waitForSelector('#top[data-initialized]')
    await page.waitForTimeout(620)
    // CDP captures the actual animation frame; Playwright screenshots await font/layout stability.
    const client = await page.context().newCDPSession(page)
    const { data } = await client.send('Page.captureScreenshot', { format: 'png' })
    await writeFile(`${out}/hero-mid-draw.png`, Buffer.from(data, 'base64'))
    await page.close()
  }
} finally {
  await browser.close()
  await stop()
}
console.log(`Screenshots: ${out}`)
