import { chromium } from '@playwright/test'
import { gzipSync } from 'node:zlib'
import { readFile, writeFile } from 'node:fs/promises'
import { productionBrowser } from '../build-browser.ts'
import { startPreview, baseURL } from '../server.ts'
const stop = await startPreview()
const browser = productionBrowser(await chromium.launch({ channel: 'chrome' }))
const results: unknown[] = []
try {
  for (const route of [
    '/docs/',
    '/docs/getting-started/install/',
    '/es/docs/getting-started/install/',
  ]) {
    const page = await browser.newPage()
    const scripts = new Set<string>()
    page.on('response', (response) => {
      const url = new URL(response.url())
      if (url.pathname.endsWith('.js')) scripts.add(url.pathname)
    })
    await page.goto(baseURL + route)
    await page.evaluate(() => document.fonts.ready)
    await page.waitForTimeout(500)
    let externalGzip = 0
    for (const path of scripts) externalGzip += gzipSync(await readFile(`dist${path}`)).length
    const html = await readFile(`dist${route}index.html`, 'utf8')
    const inline = [...html.matchAll(/<script\b[^>]*>([\s\S]*?)<\/script>/g)]
      .map((match) => match[1])
      .join('\n')
    const inlineGzip = gzipSync(inline).length
    results.push({
      route,
      externalGzip,
      inlineGzip,
      totalGzip: externalGzip + inlineGzip,
      scripts: [...scripts],
    })
    await page.close()
  }
} finally {
  await browser.close()
  await stop()
}
await writeFile(
  '/home/cristhian/projects/ui-conceptos/landing/implementacion/final/docs-size.json',
  JSON.stringify(results, null, 2),
)
console.log(JSON.stringify(results, null, 2))
