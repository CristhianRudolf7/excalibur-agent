import { expect, test } from '@playwright/test'
import { baseURL } from '../server'

for (const width of [390, 1024, 1280, 1536, 1920]) {
  test(`sample amounts reconcile and chart uses a zero baseline at ${width}`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.goto('/')
    await page.locator('#data').scrollIntoViewIfNeeded()
    await expect(page.locator('#data')).toHaveAttribute('data-initialized', 'true')
    const rows = await page.locator('.data-clean tbody tr').allTextContents()
    expect(rows).toEqual([
      'Client-01Project AS/ 1,800Oct 10',
      'Client-02Project BS/ 1,200Sep 15',
      'Client-03Project CS/ 600Oct 10',
    ])
    const amounts = await page
      .locator('.data-clean tbody tr')
      .evaluateAll((rows) =>
        rows.map((row) => Number(row.children[2]!.textContent!.replace(/\D/g, ''))),
      )
    expect(amounts.reduce((a, b) => a + b, 0)).toBe(3600)
    await expect(page.locator('[data-total]')).toHaveText('S/ 3,600')
    const bars = await page.locator('[data-bar]').evaluateAll((nodes) =>
      nodes.map((node) => ({
        height: node.getBoundingClientRect().height,
        bottom: node.getBoundingClientRect().bottom,
      })),
    )
    expect(bars[0]!.height / bars[1]!.height).toBeCloseTo(1.5, 1)
    expect(bars[0]!.height / bars[2]!.height).toBeCloseTo(3, 1)
    expect(
      Math.max(...bars.map((b) => b.bottom)) - Math.min(...bars.map((b) => b.bottom)),
    ).toBeLessThan(1)
    await expect(page.locator('.data-sample')).toBeVisible()
    await expect(page.locator('.memory-sample')).toHaveText('Sample data')
  })
}

test('sample browser keeps the password hidden and uses inert illustration controls', async ({
  page,
}) => {
  await page.goto('/')
  const browser = page.locator('.day-browser')
  await expect(browser).toHaveAttribute('role', 'img')
  await expect(browser.locator('input, button')).toHaveCount(0)
  await expect(browser.locator('[data-password-dot]')).toHaveCount(8)
  await expect(browser.locator('.day-sample')).toHaveText('Sample data')
  await expect(page.locator('.day-caption')).toHaveText(
    'What pending assignments do I have in my course?',
  )
  await expect(page.locator('.day-note-vault')).toHaveText(
    '(2) Password filled from your vault. The model never sees it.',
  )
})

test('memory owns one unbranched thread through three conversation dates', async ({ page }) => {
  await page.goto('/')
  const path = await page.locator('#memory .desktop-line [data-line-path]').getAttribute('d')
  expect(path!.match(/M/g)).toHaveLength(1)
  expect(path).toMatch(/^M218 0/)
  expect(path).toMatch(/V1024$/)
  expect(await page.locator('.conversation-date').allTextContents()).toEqual([
    'Oct 28',
    'Oct 30',
    'Nov 1',
  ])
  await expect(page.locator('.memory-citation')).toHaveText('Art. 160 · p. 27')
})

for (const javaScriptEnabled of [false, true]) {
  test(`sections retain full readable fallback with JS ${javaScriptEnabled}`, async ({
    browser,
  }) => {
    const page = await browser.newPage({ javaScriptEnabled, reducedMotion: 'reduce' })
    for (const width of [390, 1024, 1280, 1536, 1920]) {
      await page.setViewportSize({ width, height: 900 })
      await page.goto(baseURL)
      for (const name of ['your-day', 'memory', 'data']) {
        const section = page.locator(`#${name}`)
        await section.scrollIntoViewIfNeeded()
        await expect(section.locator('h2')).toBeVisible()
        const offsets = await section
          .locator('[data-line-path]')
          .evaluateAll((paths) =>
            paths.map((path) => parseFloat(getComputedStyle(path).strokeDashoffset)),
          )
        expect(offsets.every((offset) => offset === 0)).toBe(true)
      }
      await expect(page.locator('[data-word]').last()).toHaveCSS('opacity', '1')
      await expect(page.locator('[data-click-tag]')).toHaveCSS('opacity', '1')
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
        true,
      )
    }
    await page.close()
  })
}

test('local rail math agrees with native SVG geometry in both responsive drawings', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1536, height: 900 })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.goto('/')
  for (const name of ['your-day', 'memory', 'data']) {
    await page.locator(`#${name}`).scrollIntoViewIfNeeded()
    await expect(page.locator(`#${name}`)).toHaveAttribute('data-initialized', 'true')
    const errors = await page.locator(`#${name} [data-line-path]`).evaluateAll((paths) =>
      paths.map((node) => {
        const path = node as SVGPathElement
        const nativeLength = SVGGeometryElement.prototype.getTotalLength.call(path)
        const localLength = path.getTotalLength()
        let maximum = 0
        for (let index = 0; index <= 400; index++) {
          const native = SVGGeometryElement.prototype.getPointAtLength.call(
            path,
            (nativeLength * index) / 400,
          )
          const local = path.getPointAtLength((localLength * index) / 400)
          maximum = Math.max(maximum, Math.hypot(native.x - local.x, native.y - local.y))
        }
        return maximum
      }),
    )
    errors.forEach((error) => expect(error).toBeLessThan(0.5))
  }
})

test('browser actions wait for the pen and reverse when scrolling back', async ({ page }) => {
  await page.setViewportSize({ width: 1536, height: 900 })
  await page.goto('/')
  await expect(page.locator('#your-day')).toHaveAttribute('data-initialized', 'true')
  const highlight = page.locator('.day-action-highlight')
  await expect(highlight).toHaveCSS('opacity', '0')
  await page
    .locator('#your-day')
    .evaluate((el) =>
      scrollTo(0, el.getBoundingClientRect().top + scrollY + el.clientHeight * 0.05),
    )
  await expect(highlight).toHaveCSS('opacity', '1')
  await expect(page.locator('[data-password-dot]').last()).toHaveCSS('opacity', '1')
  await expect(page.locator('[data-cursor]')).toHaveCSS('opacity', '0')
  await page
    .locator('#your-day')
    .evaluate((el) =>
      scrollTo(0, el.getBoundingClientRect().top + scrollY + el.clientHeight * 0.35),
    )
  await expect(page.locator('[data-click-tag]')).toHaveCSS('opacity', '1')
  await page
    .locator('#your-day')
    .evaluate((el) => scrollTo(0, el.getBoundingClientRect().top + scrollY - innerHeight * 0.3))
  await expect(highlight).toHaveCSS('opacity', '0')
  await expect(page.locator('[data-click-tag]')).toHaveCSS('opacity', '0')
})
