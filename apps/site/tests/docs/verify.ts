/** Run after `pnpm build`: Node 22, installed Chrome, assigned port only.
 * A separate test build temporarily promotes the private draft. The source is
 * restored immediately and a normal production build is restored on exit.
 */
import { chromium, expect } from '@playwright/test'
import { readFile, writeFile, mkdir, access } from 'node:fs/promises'
import { gzipSync } from 'node:zlib'
import { spawn } from 'node:child_process'
import { startPreview, baseURL } from '../server.ts'
import { tokenContrast } from './contrast.ts'

const out =
  process.env.DOCS_SHOTS_DIR ?? '/home/cristhian/projects/ui-conceptos/landing/implementacion/docs'
const fixture = 'src/content/docs/docs/style-guide.mdx'
const source = await readFile(fixture, 'utf8')
const report: Record<string, unknown> = {
  screenshots: out,
  notes: [
    'Chrome headless with software rendering; paint costs can exceed a hardware-rendered browser.',
    'Article screenshots use a test-only production build with the draft temporarily promoted.',
  ],
}
await mkdir(out, { recursive: true })
async function build() {
  await new Promise<void>((resolve, reject) => {
    const child = spawn(
      'nice',
      ['-n', '10', process.execPath, 'node_modules/astro/bin/astro.mjs', 'build'],
      { stdio: 'inherit' },
    )
    child.once('exit', (code) =>
      code === 0 ? resolve() : reject(new Error(`Build failed: ${code}`)),
    )
    child.once('error', reject)
  })
}
async function missingDraft() {
  let present = false
  try {
    await access('dist/docs/style-guide/index.html')
    present = true
  } catch {
    /* Expected. */
  }
  expect(present, 'Draft must not ship').toBe(false)
  await access('dist/pagefind/pagefind.js')
  expect(await readFile('dist/sitemap-0.xml', 'utf8')).not.toContain('style-guide')
  report.production = { draftExcluded: true, searchIndex: true }
}
await missingDraft()
report.tokenContrast = await tokenContrast()
let stop: (() => Promise<void>) | undefined
let browser: Awaited<ReturnType<typeof chromium.launch>> | undefined
try {
  await writeFile(
    fixture,
    source.replace('draft: true', 'draft: false').replace('hidden: true', 'hidden: false'),
  )
  try {
    await build()
  } finally {
    await writeFile(fixture, source)
  }
  stop = await startPreview()
  browser = await chromium.launch({ channel: 'chrome' })
  const errors: string[] = []
  const initialScripts = new Set<string>()
  const searchScripts = new Set<string>()
  for (const width of [1280, 1536, 390]) {
    for (const theme of ['light', 'dark'] as const) {
      const context = await browser.newContext({
        viewport: { width, height: 1024 },
        colorScheme: theme,
        reducedMotion: 'reduce',
      })
      const page = await context.newPage()
      page.on('pageerror', (error) => errors.push(error.message))
      let collectScripts = true
      const pageScripts = new Set<string>()
      page.on('response', (response) => {
        if (collectScripts && /\.m?js(?:$|\?)/.test(response.url())) {
          searchScripts.add(response.url())
          pageScripts.add(response.url())
        }
      })
      await page.goto(`${baseURL}/docs/`)
      await page.evaluate(() => document.fonts.ready)
      await expect(page.locator('h1')).toHaveText('The manual.')
      await expect(page.locator('html')).toHaveAttribute('data-theme', theme)
      for (const url of pageScripts) initialScripts.add(url)
      await page.screenshot({ path: `${out}/home-${width}-${theme}.png`, fullPage: true })
      if (width >= 1024) {
        await page.locator('.manual-area').first().focus()
        const label = page.locator('.manual-area').first().locator('svg text')
        const fontSize = await label.evaluate(
          (el) =>
            (parseFloat(getComputedStyle(el).fontSize) * el.getBoundingClientRect().height) /
            (el as SVGGraphicsElement).getBBox().height,
        )
        expect(fontSize).toBeGreaterThanOrEqual(18.67)
        await page.screenshot({ path: `${out}/home-focus-${width}-${theme}.png` })
      }
      for (const route of ['/docs/', '/es/docs/', '/docs/style-guide/']) {
        await page.goto(`${baseURL}${route}`)
        await page.evaluate(() => document.fonts.ready)
        expect(await page.locator('h1').count()).toBe(1)
        expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
          true,
        )
        const failures = await page.evaluate(() => {
          function rgb(value: string) {
            return (
              value
                .match(/[\d.]+/g)
                ?.slice(0, 3)
                .map(Number) ?? [0, 0, 0]
            )
          }
          function lum(color: number[]) {
            return color
              .map((v) => v / 255)
              .map((v) => (v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4))
              .reduce((s, v, i) => s + v * [0.2126, 0.7152, 0.0722][i], 0)
          }
          const failures: string[] = []
          for (const element of document.querySelectorAll<HTMLElement>('body *')) {
            if (!element.checkVisibility({ checkOpacity: true, checkVisibilityCSS: true })) continue
            if (
              !Array.from(element.childNodes).some(
                (node) => node.nodeType === Node.TEXT_NODE && node.textContent?.trim(),
              )
            )
              continue
            if (element.closest('svg, .sr-only, script, style, template')) continue
            const rect = element.getBoundingClientRect()
            if (rect.width <= 1 || rect.height <= 1) continue
            const css = getComputedStyle(element)
            let ancestor: Element | null = element
            let background = ''
            while (ancestor) {
              background = getComputedStyle(ancestor).backgroundColor
              if (!['transparent', 'rgba(0, 0, 0, 0)'].includes(background)) break
              ancestor = ancestor.parentElement
            }
            const a = lum(rgb(css.color)),
              b = lum(rgb(background))
            const ratio = (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05)
            const large =
              parseFloat(css.fontSize) >= 24 ||
              (parseFloat(css.fontSize) >= 18.67 && parseFloat(css.fontWeight) >= 700)
            if (ratio < (large ? 3 : 4.5))
              failures.push(
                `${element.tagName}.${element.className}: ${element.textContent?.trim().slice(0, 45)} (${ratio.toFixed(2)}) ${css.color} on ${background}`,
              )
          }
          return failures
        })
        expect(failures, `${route}, ${theme}, ${width}: text contrast`).toEqual([])
      }
      if (width >= 800)
        await expect(page.locator('.sidebar-content a[aria-current="page"]')).toBeVisible()
      await page.screenshot({ path: `${out}/article-${width}-${theme}.png` })
      await page.screenshot({ path: `${out}/article-full-${width}-${theme}.png`, fullPage: true })
      expect(
        await page.evaluate(
          () =>
            document.getAnimations().filter((animation) => animation.playState === 'running')
              .length,
        ),
      ).toBe(0)
      if (width === 1536) {
        for (const heading of [
          'notes-from-the-blueprint',
          'code-you-can-read',
          'follow-the-stations',
          'tables-and-records',
        ]) {
          await page
            .locator(`#${heading}`)
            .evaluate((heading) => heading.scrollIntoView({ block: 'start' }))
          await page.waitForTimeout(120)
          await page.screenshot({ path: `${out}/${heading}-${theme}.png` })
        }
      }
      // Native tabs, copy, and TOC retain their behavior after restyling.
      await page.getByRole('tab', { name: 'Details', exact: true }).click()
      await expect(page.getByRole('tab', { name: 'Details', exact: true })).toHaveAttribute(
        'aria-selected',
        'true',
      )
      await page.keyboard.press('ArrowLeft')
      await expect(page.getByRole('tab', { name: 'Overview', exact: true })).toHaveAttribute(
        'aria-selected',
        'true',
      )
      await context.grantPermissions(['clipboard-read', 'clipboard-write'])
      await page.locator('.expressive-code .copy button').first().click()
      expect(await page.evaluate(() => navigator.clipboard.readText())).toContain(
        'export const settings',
      )
      if (width >= 1024) {
        const link = page.locator('[data-manual-toc] a[href="#code-you-can-read"]')
        await link.click()
        await expect(link).toHaveAttribute('aria-current', 'true')
        const delta = await page.locator('[data-manual-toc]').evaluate((toc) => {
          const link = toc.querySelector('a[aria-current="true"]')!
          const square = toc.querySelector('.manual-toc-pen')!
          return Math.abs(
            square.getBoundingClientRect().top +
              3.5 -
              (link.getBoundingClientRect().top + link.getBoundingClientRect().height / 2),
          )
        })
        expect(delta).toBeLessThanOrEqual(1)
      }
      await page.locator('main').evaluate((el) => el.scrollIntoView())
      await page.keyboard.press('Control+k')
      await expect(page.locator('site-search dialog')).toBeVisible()
      expect(
        await page
          .locator('site-search dialog')
          .evaluate((el) => getComputedStyle(el, '::backdrop').backdropFilter),
      ).toBe('none')
      await page.locator('.pagefind-ui__search-input').fill('manual')
      await expect(page.locator('.pagefind-ui__result').first()).toBeVisible()
      await page.screenshot({ path: `${out}/search-${width}-${theme}.png` })
      await page.keyboard.press('Escape')
      await expect(page.locator('site-search dialog')).not.toBeVisible()
      await expect(page.locator('[data-open-modal]')).toBeFocused()
      if (width === 390) {
        await page.locator('[popovertarget="starlight__sidebar"]').click()
        await expect(page.locator('#starlight__sidebar')).toBeVisible()
        await page.screenshot({ path: `${out}/sidebar-${width}-${theme}.png` })
        await page.keyboard.press('Escape')
      }
      collectScripts = false
      // Shared theme key survives a full navigation to the landing and back.
      await page.locator('.manual-header [data-theme-toggle]').click()
      const changed = theme === 'light' ? 'dark' : 'light'
      expect(await page.evaluate(() => localStorage.getItem('starlight-theme'))).toBe(changed)
      await page.goto(baseURL)
      await expect(page.locator('html')).toHaveAttribute('data-theme', changed)
      await page.goto(`${baseURL}/docs/`)
      await expect(page.locator('html')).toHaveAttribute('data-theme', changed)
      await page.locator('.manual-language select').selectOption('/es/docs/')
      await expect(page).toHaveURL(/\/es\/docs\/$/)
      await expect(page.locator('h1')).toHaveText('El manual.')
      await page.keyboard.press('Tab')
      await expect(page.locator('.sl-skip-link')).toBeFocused()
      expect(
        await page.locator('.sl-skip-link').evaluate((el) => getComputedStyle(el).outlineStyle),
      ).toBe('solid')
      await context.close()
    }
  }
  // Extra boundary widths protect the header and text from accidental overflow.
  const boundary = await browser.newPage()
  for (const width of [360, 1024, 1366, 1920]) {
    await boundary.setViewportSize({ width, height: 1024 })
    for (const route of ['/docs/', '/docs/style-guide/']) {
      await boundary.goto(`${baseURL}${route}`)
      expect(
        await boundary.evaluate(() => document.documentElement.scrollWidth <= innerWidth),
      ).toBe(true)
    }
  }
  await boundary.close()
  for (const theme of ['light', 'dark'] as const) {
    const noJS = await browser.newContext({
      javaScriptEnabled: false,
      colorScheme: theme,
      viewport: { width: 390, height: 844 },
    })
    const noJSPage = await noJS.newPage()
    await noJSPage.goto(`${baseURL}/docs/style-guide/`)
    await expect(noJSPage.locator('h1')).toBeVisible()
    const bg = await noJSPage.locator('body').evaluate((el) => getComputedStyle(el).backgroundColor)
    expect(bg).toBe(theme === 'light' ? 'rgb(255, 255, 255)' : 'rgb(11, 18, 32)')
    await noJSPage.screenshot({ path: `${out}/no-js-article-${theme}.png` })
    await noJSPage.locator('details summary').last().click()
    await expect(noJSPage.getByText('First supporting detail.')).toBeVisible()
    await noJSPage.locator('[popovertarget="starlight__sidebar"]').click()
    await expect(noJSPage.locator('#starlight__sidebar')).toBeVisible()
    await noJS.close()
  }

  const perf = await browser.newPage({
    viewport: { width: 1536, height: 1024 },
    reducedMotion: 'no-preference',
    colorScheme: 'dark',
  })
  const cdp = await perf.context().newCDPSession(perf)
  await cdp.send('Emulation.setCPUThrottlingRate', { rate: 4 })
  const articleScripts = new Set<string>()
  perf.on('response', (response) => {
    if (/\.m?js(?:$|\?)/.test(response.url())) articleScripts.add(response.url())
  })
  await perf.goto(`${baseURL}/docs/style-guide/`)
  await perf.evaluate(() => document.fonts.ready)
  await perf.waitForTimeout(500)
  const performance = await perf.evaluate(async () => {
    const frames: number[] = [],
      tasks: number[] = []
    let cls = 0
    const observer = new PerformanceObserver((list) => {
      for (const entry of list.getEntries()) {
        if (entry.entryType === 'longtask') tasks.push(entry.duration)
        if (
          entry.entryType === 'layout-shift' &&
          !(entry as PerformanceEntry & { hadRecentInput: boolean }).hadRecentInput
        )
          cls += (entry as PerformanceEntry & { value: number }).value
      }
    })
    observer.observe({ entryTypes: ['longtask', 'layout-shift'] })
    await new Promise<void>((resolve) => {
      let start = 0,
        previous = 0
      function frame(now: number) {
        if (!start) {
          start = now
          previous = now
        }
        frames.push(now - previous)
        previous = now
        const progress = Math.min(1, (now - start) / 10000)
        scrollTo(0, (document.documentElement.scrollHeight - innerHeight) * progress)
        if (progress < 1) requestAnimationFrame(frame)
        else resolve()
      }
      requestAnimationFrame(frame)
    })
    observer.disconnect()
    frames.sort((a, b) => a - b)
    return {
      p50: frames[Math.floor(frames.length * 0.5)],
      p95: frames[Math.floor(frames.length * 0.95)],
      longestTask: Math.max(0, ...tasks),
      cls,
      domNodes: document.querySelectorAll('*').length,
    }
  })
  report.performance4x = performance
  const tocTransform = await perf
    .locator('[data-manual-toc]')
    .evaluate((toc) => getComputedStyle(toc).getPropertyValue('--toc-y'))
  expect(parseFloat(tocTransform)).toBeGreaterThan(100)
  await perf.screenshot({ path: `${out}/motion-end-dark.png` })
  await perf.close()
  async function bytes(urls: Set<string>) {
    let total = 0
    for (const url of urls) {
      const pathname = new URL(url).pathname
      const data = await readFile(`dist${pathname}`)
      expect(data.toString(), `Docs must not load GSAP: ${pathname}`).not.toMatch(
        /ScrollTrigger|gsap-core/,
      )
      total += gzipSync(data).length
    }
    return total
  }
  const homeHTML = await readFile('dist/docs/index.html', 'utf8')
  const inline = [...homeHTML.matchAll(/<script\b[^>]*>([\s\S]*?)<\/script>/g)]
    .map((m) => m[1])
    .join('\n')
  report.javascript = {
    initialExternalGzipBytes: await bytes(initialScripts),
    allExternalAfterSearchGzipBytes: await bytes(searchScripts),
    inlineGzipBytes: gzipSync(inline).length,
    articleExternalGzipBytes: await bytes(articleScripts),
    articleInlineGzipBytes: gzipSync(
      [
        ...(await readFile('dist/docs/style-guide/index.html', 'utf8')).matchAll(
          /<script\b[^>]*>([\s\S]*?)<\/script>/g,
        ),
      ]
        .map((m) => m[1])
        .join('\n'),
    ).length,
    customEnhancementGzipBytes: gzipSync(
      [...homeHTML.matchAll(/<script\b[^>]*type="module"[^>]*>([\s\S]*?)<\/script>/g)]
        .map((m) => m[1])
        .filter((code) => code.includes('starlight-theme'))
        .join('\n'),
    ).length,
  }
  expect(
    (report.javascript as { customEnhancementGzipBytes: number }).customEnhancementGzipBytes,
  ).toBeLessThanOrEqual(6144)
  expect(performance.longestTask).toBeLessThanOrEqual(100)
  expect(performance.cls).toBeLessThanOrEqual(0.02)
  expect(errors).toEqual([])
  report.runtimeErrors = errors
  report.markup = {
    homeGzipBytes: gzipSync(homeHTML).length,
    source: 'Production compilation; draft promoted only in the test build',
  }
  report.sidebarSource = {
    sharedModulePresent: await access('src/docs-sidebar.mjs').then(
      () => true,
      () => false,
    ),
  }
  report.checks = [
    'Both locales',
    'Both themes',
    '1280/1536/390 screenshots',
    '360/1024/1366/1920 overflow',
    'Visible text AA contrast',
    'Search results and Escape/focus return',
    'Keyboard focus',
    'Native mobile sidebar',
    'No JS disclosure/sidebar',
    'Shared landing theme persistence',
  ]
} finally {
  await writeFile(fixture, source)
  await browser?.close()
  await stop?.()
  await build()
  await missingDraft()
  await writeFile(`${out}/measurements.json`, JSON.stringify(report, null, 2) + '\n')
}
console.log(JSON.stringify(report, null, 2))
