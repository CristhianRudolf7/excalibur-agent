import { chromium } from '@playwright/test'
import sharp from 'sharp'
import { writeFile, mkdir } from 'node:fs/promises'
import { productionBrowser } from '../build-browser.ts'
import { startPreview, baseURL } from '../server.ts'
const stop = await startPreview()
const browser = productionBrowser(await chromium.launch({ channel: 'chrome' }))
const out = process.env.QA_OUT ?? 'test-results/shots/final'
const report: {
  section: string
  width: number
  theme: string
  differentPixels: number
  meanChannelError: number
}[] = []
await mkdir(`${out}/contained`, { recursive: true })
try {
  for (const theme of ['light', 'dark'] as const) {
    for (const width of [1280, 1536, 1920, 390]) {
      const page = await browser.newPage({
        viewport: { width, height: width === 390 ? 844 : (width * 9) / 16 },
        colorScheme: theme,
        reducedMotion: 'reduce',
      })
      await page.goto(baseURL)
      await page.evaluate(() => document.fonts.ready)
      for (const section of [
        'hero',
        'your-day',
        'memory',
        'data',
        'code',
        'work',
        'machine',
        'measured',
        'install',
        'extensions',
      ]) {
        const node = page.locator(`#${section === 'hero' ? 'top' : section}`)
        await node.scrollIntoViewIfNeeded()
        await page.waitForTimeout(100)
        const current = await node.screenshot()
        const a = await sharp(current).ensureAlpha().raw().toBuffer({ resolveWithObject: true })
        const b = await sharp(`${out}/${section}-${width}-${theme}.png`)
          .ensureAlpha()
          .raw()
          .toBuffer({ resolveWithObject: true })
        let differentPixels = 0,
          channelError = 0
        if (a.info.width !== b.info.width || a.info.height !== b.info.height) differentPixels = -1
        else
          for (let i = 0; i < a.data.length; i += 4)
            if (
              a.data[i] !== b.data[i] ||
              a.data[i + 1] !== b.data[i + 1] ||
              a.data[i + 2] !== b.data[i + 2]
            )
              differentPixels++
        if (differentPixels >= 0)
          for (let i = 0; i < a.data.length; i += 4)
            for (let channel = 0; channel < 3; channel++)
              channelError += Math.abs(a.data[i + channel]! - b.data[i + channel]!)
        const meanChannelError = channelError / (a.info.width * a.info.height * 3 * 255)
        if (differentPixels)
          await writeFile(`${out}/contained/${section}-${width}-${theme}.png`, current)
        report.push({ section, width, theme, differentPixels, meanChannelError })
      }
      await page.close()
    }
  }
} finally {
  await browser.close()
  await stop()
  await writeFile(`${out}/containment-pixels.json`, JSON.stringify(report, null, 2))
}
const changed = report.filter((item) => item.differentPixels !== 0)
console.log(JSON.stringify({ compared: report.length, changed }, null, 2))
// Compositor containment can change fractional text antialiasing. Dimensions
// and large pixel changes are assertions; smaller deltas are saved for review.
if (changed.some((item) => item.differentPixels < 0 || item.meanChannelError > 0.01))
  process.exitCode = 1
