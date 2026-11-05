import { productionBrowser } from '../build-browser.ts'
import assert from 'node:assert/strict'
import { mkdir, writeFile } from 'node:fs/promises'
import { chromium } from '@playwright/test'
import { docsSidebar } from '../../src/docs-sidebar.mjs'
import { baseURL, startPreview } from '../server.ts'

// Use the assigned production-preview port. The server helper records and stops
// only the PID that it starts. No browser or daemon installation is performed.
assert(process.env.SITE_PORT, 'Set SITE_PORT to the assigned preview port.')
const stop = await startPreview()
let browser
const output = 'test-results/content'
const routes = docsSidebar.flatMap((group) => group.items)
const examples = new Set([
  'docs/getting-started/install',
  'docs/data/import-spreadsheet',
  'docs/building/agents',
  'docs/reference/cli',
])
const report = { views: 0, maxOverflow: 0, screenshots: [], errors: [], shellBrokenLinks: [] }
try {
  await mkdir(output, { recursive: true })
  browser = productionBrowser(await chromium.launch({ channel: 'chrome', headless: true }))
  for (const theme of ['light', 'dark']) {
    for (const width of [360, 390, 1024, 1280, 1366, 1536, 1920]) {
      const context = await browser.newContext({
        viewport: { width, height: width === 390 ? 844 : 1024 },
        colorScheme: theme,
        reducedMotion: 'reduce',
      })
      await context.addInitScript((selected) => {
        localStorage.setItem('starlight-theme', selected)
      }, theme)
      const page = await context.newPage()
      page.on('pageerror', (error) => report.errors.push(error.message))
      for (const locale of ['', 'es/']) {
        for (const route of routes) {
          const path = `/${locale}${route}/`
          const response = await page.goto(`${baseURL}${path}`)
          assert.equal(response.status(), 200, path)
          await page.evaluate(() => document.fonts.ready)
          assert.equal(await page.locator('h1').count(), 1, path)
          assert.equal(await page.locator('html').getAttribute('data-theme'), theme)
          assert.equal(await page.locator('html').getAttribute('lang'), locale ? 'es' : 'en')
          const overflow = await page.evaluate(
            () => document.documentElement.scrollWidth - window.innerWidth,
          )
          report.maxOverflow = Math.max(report.maxOverflow, overflow)
          assert(overflow <= 1, `${path} at ${width}: ${overflow}px horizontal overflow`)
          assert((await page.locator('.sl-markdown-content').innerText()).length > 600, path)
          report.views++
          if ([390, 1536].includes(width) && examples.has(route)) {
            const name = `${locale ? 'es' : 'en'}-${route.split('/').at(-1)}-${theme}-${width}.png`
            await page.screenshot({ path: `${output}/${name}`, fullPage: true })
            report.screenshots.push(`${output}/${name}`)
          }
        }
      }
      await context.close()
      console.log(`Checked both locales at ${width}px in ${theme} mode.`)
    }
  }
  // Record shell links separately: this worker owns article content, not the
  // Starlight header, index, config integration, or locale home routes.
  const page = await browser.newPage()
  await page.goto(`${baseURL}/es/docs/getting-started/install/`)
  const links = await page
    .locator('header a[href^="/"]')
    .evaluateAll((nodes) => nodes.map((node) => node.getAttribute('href')))
  for (const href of new Set(links)) {
    const response = await page.request.get(`${baseURL}${href}`)
    if (!response.ok()) report.shellBrokenLinks.push({ href, status: response.status() })
  }
  assert.deepEqual(report.errors, [])
  console.log(JSON.stringify(report, null, 2))
} finally {
  await writeFile(`${output}/report.json`, JSON.stringify(report, null, 2) + '\n')
  await browser?.close()
  await stop()
}
