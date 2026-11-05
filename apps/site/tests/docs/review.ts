/** Capture the normal production splash, then make local art-direction sheets. */
import { chromium } from '@playwright/test'
import { readFile } from 'node:fs/promises'
import { startPreview, baseURL } from '../server.ts'

const out =
  process.env.DOCS_SHOTS_DIR ??
  '/home/cristhian/projects/ui-conceptos/landing/implementacion/docs/accepted'
const stop = await startPreview()
const browser = await chromium.launch({ channel: 'chrome' })
try {
  for (const width of [1280, 1536, 390]) {
    for (const theme of ['light', 'dark'] as const) {
      const page = await browser.newPage({
        viewport: { width, height: 1024 },
        colorScheme: theme,
        reducedMotion: 'reduce',
      })
      await page.goto(`${baseURL}/docs/`)
      await page.evaluate(() => document.fonts.ready)
      await page.screenshot({
        path: `${out}/production-home-${width}-${theme}.png`,
        fullPage: true,
      })
      await page.goto(`${baseURL}/es/docs/`)
      await page.screenshot({
        path: `${out}/production-home-es-${width}-${theme}.png`,
        fullPage: true,
      })
      await page.close()
    }
  }
  const reference = '/home/cristhian/projects/ui-conceptos/landing'
  for (const theme of ['light', 'dark']) {
    const paths = [
      `${reference}/landing-4-blueprint.png`,
      `${out}/production-home-1536-${theme}.png`,
      `${reference}/resto/seccion-6-benchmark.png`,
      `${out}/article-1536-${theme}.png`,
    ]
    const images = await Promise.all(
      paths.map(
        async (path) => `data:image/png;base64,${(await readFile(path)).toString('base64')}`,
      ),
    )
    const sheet = await browser.newPage({ viewport: { width: 1600, height: 1250 } })
    // This sheet is a local review artifact. Reference rasters never enter dist.
    await sheet.setContent(
      `<html><body style="margin:0;background:white;display:grid;grid-template-columns:1fr 1fr;gap:12px;font:14px monospace">${images.map((src, i) => `<figure style="margin:0"><figcaption style="padding:8px">${i % 2 === 0 ? 'Approved concept' : `Implementation / ${theme}`}</figcaption><img style="width:100%;display:block" src="${src}"></figure>`).join('')}</body></html>`,
    )
    await sheet
      .locator('img')
      .evaluateAll((images) =>
        Promise.all(images.map((image) => (image as HTMLImageElement).decode())),
      )
    await sheet.waitForTimeout(200)
    await sheet.screenshot({ path: `${out}/comparison-${theme}-review.png`, fullPage: true })
    await sheet.close()
  }
} finally {
  await browser.close()
  await stop()
}
