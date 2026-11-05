import { chromium } from '@playwright/test'
import { productionBrowser } from '../build-browser.ts'
import { startPreview, baseURL } from '../server.ts'
import { writeFile } from 'node:fs/promises'
const stop = await startPreview()
const browser = productionBrowser(await chromium.launch({ channel: 'chrome' }))
const report: unknown[] = []
try {
  for (const width of (process.env.QA_WIDTHS ?? '1280,1536,1920,390').split(',').map(Number)) {
    const page = await browser.newPage({
      viewport: { width, height: width === 390 ? 844 : (width * 9) / 16 },
    })
    await page.goto(baseURL)
    await page.waitForTimeout(1800)
    const distance = await page.evaluate(() => document.documentElement.scrollHeight - innerHeight)
    const failures: unknown[] = []
    for (let y = 200; y < distance; y += 200) {
      await page.evaluate((y) => scrollTo(0, y), y)
      await page.waitForTimeout(280)
      const pens = await page.locator('[data-pen]').evaluateAll((nodes) =>
        nodes.flatMap((node) => {
          const r = node.getBoundingClientRect()
          if (!r.width || parseFloat(getComputedStyle(node).opacity) === 0) return []
          return [
            {
              section: node.closest('[data-section]')!.id,
              y: r.top,
              visible: r.bottom > 0 && r.top < innerHeight,
            },
          ]
        }),
      )
      if (!pens.some((p) => p.visible)) {
        const stopped = await page.locator('#extensions').evaluate((section) => {
          const path = section.querySelector<SVGPathElement>(
            innerWidth >= 1024 ? '.desktop-line [data-line-path]' : '.mobile-line [data-line-path]',
          )!
          return parseFloat(path.style.strokeDashoffset) === 0
        })
        if (!stopped) failures.push({ scroll: y, pens })
      }
    }
    report.push({ width, samples: Math.floor(distance / 200), failures })
    await page.close()
  }
} finally {
  await writeFile('test-results/pen-audit.json', JSON.stringify(report, null, 2))
  await browser.close()
  await stop()
}
console.log(JSON.stringify(report, null, 2))

if (report.some((item) => (item as { failures: unknown[] }).failures.length)) process.exitCode = 1
