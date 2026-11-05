import { chromium } from '@playwright/test'
import { mkdir, writeFile } from 'node:fs/promises'
import { baseURL } from '../server.ts'
import { serveBuild } from './static.ts'

const names = (process.env.B_SECTIONS ?? 'code,work,machine,measured,extensions').split(',')
const out = '/home/cristhian/projects/ui-conceptos/landing/implementacion/b'
const widths = (process.env.B_WIDTHS ?? '1024,1280,1536,1920,390').split(',').map(Number)

const browser = await chromium.launch({ channel: 'chrome' })
await mkdir(out, { recursive: true })
try {
  for (const theme of ['light', 'dark'] as const)
    for (const width of widths) {
      const page = await browser.newPage({
        viewport: { width, height: width === 390 ? 844 : 1024 },
        colorScheme: theme,
        reducedMotion: 'reduce',
      })
      await serveBuild(page.context())
      await page.goto(baseURL)
      if (!(await page.locator('.code-circuit').count()))
        throw new Error('Assigned preview is serving another build; refusing stale screenshots')
      await page.evaluate(() => document.fonts.ready)
      for (const name of names) {
        await page.locator(`#${name}`).evaluate((el) => el.scrollIntoView({ block: 'start' }))
        await page.waitForTimeout(180)
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
} finally {
  await browser.close()
}
console.log(out)
