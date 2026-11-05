import { productionBrowser } from '../build-browser.ts'
import { chromium } from '@playwright/test'
import { mkdir, writeFile } from 'node:fs/promises'
import { startPreview, baseURL } from '../server.ts'

const out = '/home/cristhian/projects/ui-conceptos/landing/implementacion/a'
await mkdir(out, { recursive: true })
const stop = await startPreview()
const browser = productionBrowser(await chromium.launch({ channel: 'chrome' }))
try {
  for (const theme of ['light', 'dark'] as const) {
    for (const width of [1024, 1280, 1536, 1920, 390]) {
      const page = await browser.newPage({
        viewport: { width, height: 900 },
        colorScheme: theme,
        reducedMotion: 'reduce',
      })
      await page.goto(baseURL)
      await page.evaluate(() => document.fonts.ready)
      for (const name of ['your-day', 'memory', 'data']) {
        await page
          .locator(`#${name}`)
          .evaluate((el) => scrollTo(0, el.getBoundingClientRect().top + scrollY))
        await page.waitForTimeout(180)
        const clip = await page.locator(`#${name}`).evaluate((el) => {
          const rect = el.getBoundingClientRect()
          return {
            x: rect.left + scrollX,
            y: rect.top + scrollY,
            width: rect.width,
            height: rect.height,
            scale: 1,
          }
        })
        const client = await page.context().newCDPSession(page)
        const { data } = await client.send('Page.captureScreenshot', {
          format: 'png',
          clip,
          captureBeyondViewport: true,
        })
        await writeFile(`${out}/${name}-${width}-${theme}.png`, Buffer.from(data, 'base64'))
        await client.detach()
      }
      await page.close()
    }
    for (const mode of ['no-js', 'mid-animation'] as const) {
      for (const width of [1536, 390]) {
        const page = await browser.newPage({
          viewport: { width, height: width === 390 ? 844 : 1000 },
          colorScheme: theme,
          javaScriptEnabled: mode !== 'no-js',
        })
        await page.goto(baseURL)
        await page.evaluate(() => document.fonts.ready)
        for (const name of ['your-day', 'memory', 'data']) {
          await page.evaluate(
            ({ name, mode }) => {
              const section = document.getElementById(name)!
              const y = section.getBoundingClientRect().top + scrollY
              scrollTo(0, y + (mode === 'mid-animation' ? section.clientHeight * 0.08 : 0))
            },
            { name, mode },
          )
          await page.waitForTimeout(mode === 'no-js' ? 100 : 450)
          const client = await page.context().newCDPSession(page)
          const clip = await page.locator(`#${name}`).evaluate((el) => {
            const r = el.getBoundingClientRect()
            return {
              x: r.left + scrollX,
              y: r.top + scrollY,
              width: r.width,
              height: r.height,
              scale: 1,
            }
          })
          const { data } = await client.send(
            'Page.captureScreenshot',
            mode === 'mid-animation'
              ? { format: 'png' }
              : { format: 'png', clip, captureBeyondViewport: true },
          )
          await writeFile(
            `${out}/${name}-${mode}${width === 390 ? '-390' : ''}-${theme}.png`,
            Buffer.from(data, 'base64'),
          )
          await client.detach()
        }
        await page.close()
      }
    }
  }
} finally {
  await browser.close()
  await stop()
}
console.log(`Screenshots saved in ${out}`)
