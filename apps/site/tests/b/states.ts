import { productionBrowser } from '../build-browser.ts'
import { chromium } from '@playwright/test'
import { writeFile, mkdir } from 'node:fs/promises'
import { serveBuild } from './static.ts'
import { baseURL } from '../server.ts'

const names = (process.env.B_SECTIONS ?? 'code,work,machine,measured,extensions').split(',')
const out = '/home/cristhian/projects/ui-conceptos/landing/implementacion/b'
await mkdir(out, { recursive: true })
const browser = productionBrowser(await chromium.launch({ channel: 'chrome' }))
async function capture(page: import('@playwright/test').Page, name: string, file: string) {
  if (name === 'machine' && page.viewportSize()!.width >= 1024 && !file.includes('no-js')) {
    await page.screenshot({ path: `${out}/${file}.png` })
    return
  }
  const client = await page.context().newCDPSession(page)
  const clip = await page
    .locator(
      name === 'machine' && page.viewportSize()!.width >= 1024 ? '.machine-canvas' : `#${name}`,
    )
    .evaluate((el) => {
      const r = el.getBoundingClientRect()
      return { x: r.left + scrollX, y: r.top + scrollY, width: r.width, height: r.height, scale: 1 }
    })
  const { data } = await client.send('Page.captureScreenshot', {
    format: 'png',
    clip,
    captureBeyondViewport: true,
  })
  await writeFile(`${out}/${file}.png`, Buffer.from(data, 'base64'))
  await client.detach()
}
try {
  for (const theme of (process.env.B_THEMES ?? 'light,dark').split(',') as ('light' | 'dark')[]) {
    for (const name of process.env.B_NO_JS_ONLY === '1' ? [] : names) {
      const page = await browser.newPage({
        viewport: { width: 1536, height: 1024 },
        colorScheme: theme,
      })
      await serveBuild(page.context())
      await page.goto(`${baseURL}/#${name}`)
      await page.evaluate(() => document.fonts.ready)
      await page.waitForSelector(`#${name}[data-initialized]`)
      await page.waitForTimeout(500)
      const target = await page.locator(`#${name}`).evaluate((el, name) => {
        const top = el.getBoundingClientRect().top + scrollY
        if (name === 'machine') return top + innerHeight * 0.78
        const fraction = name === 'measured' ? 0.48 : name === 'extensions' ? 0.55 : 0.53
        return top + fraction * el.getBoundingClientRect().height - innerHeight * 0.62
      }, name)
      await page.evaluate((y) => scrollTo(0, y), target)
      await page.waitForTimeout(350)
      await capture(page, name, `${name}-1536-${theme}-mid`)
      if (name === 'machine') {
        const end = await page
          .locator('.machine-track')
          .evaluate((el) => el.getBoundingClientRect().top + scrollY + innerHeight * 1.5)
        await page.evaluate((y) => scrollTo(0, y), end)
        await page.waitForTimeout(850)
        await capture(page, name, `${name}-1536-${theme}-seated`)
      }
      if (name === 'extensions') {
        await page.evaluate(() => scrollTo(0, document.documentElement.scrollHeight - innerHeight))
        await page.waitForTimeout(850)
        await capture(page, name, `${name}-1536-${theme}-finished`)
      }
      await page.close()
    }
    for (const width of (process.env.B_WIDTHS ?? '390,1536').split(',').map(Number)) {
      const page = await browser.newPage({
        viewport: { width, height: 1024 },
        colorScheme: theme,
        javaScriptEnabled: false,
      })
      await serveBuild(page.context())
      await page.goto(baseURL)
      await page.evaluate(() => document.fonts.ready)
      for (const name of names) {
        await page.locator(`#${name}`).evaluate((el) => el.scrollIntoView({ block: 'start' }))
        await capture(page, name, `${name}-${width}-${theme}-no-js`)
      }
      await page.close()
    }
  }
} finally {
  await browser.close()
}
console.log(`Motion and no-JS screenshots: ${out}`)
