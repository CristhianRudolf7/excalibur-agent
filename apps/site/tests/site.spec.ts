import { expect, test } from './browser'
import { baseURL } from './server'

for (const width of [1280, 1536, 1920]) {
  test(`rail joins stay within one pixel at ${width}`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.goto('/')
    const errors = await page.evaluate(() => {
      const paths = [...document.querySelectorAll<SVGPathElement>('.desktop-line [data-line-path]')]
      const point = (path: SVGPathElement, end: boolean) => {
        const p = path.getPointAtLength(end ? path.getTotalLength() : 0)
        return new DOMPoint(p.x, p.y).matrixTransform(path.getScreenCTM()!)
      }
      return paths.slice(1).flatMap((path, i) => {
        const a = point(paths[i]!, true),
          b = point(path, false)
        return Math.hypot(a.x - b.x, a.y - b.y) > 1
          ? [`${i}: (${a.x},${a.y}) → (${b.x},${b.y})`]
          : []
      })
    })
    expect(errors).toEqual([])
  })
}
for (const width of [360, 390, 768, 1024, 1280, 1366, 1536, 1920]) {
  for (const theme of ['light', 'dark'] as const) {
    test(`readable and clean at ${width}, ${theme}`, async ({ page }) => {
      const errors: string[] = []
      page.on('pageerror', (error) => errors.push(error.message))
      page.on('console', (message) => {
        if (message.type() === 'error') errors.push(message.text())
      })
      page.on('response', (response) => {
        if (response.status() >= 400) errors.push(`${response.status()} ${response.url()}`)
      })
      await page.setViewportSize({ width, height: 900 })
      await page.emulateMedia({ colorScheme: theme })
      await page.goto('/')
      await page.evaluate(() => document.fonts.ready)
      await page.waitForTimeout(1800)
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
        true,
      )
      expect(await page.locator('html').getAttribute('data-theme')).toBe(theme)
      const collisions = await page.evaluate(() => {
        const nodes = [...document.querySelectorAll<HTMLElement>('[data-noverlap]')].filter(
          (el) => el.getBoundingClientRect().width > 0,
        )
        return nodes.flatMap((a, i) =>
          nodes.slice(i + 1).flatMap((b) => {
            if (a.contains(b) || b.contains(a)) return []
            const x = a.getBoundingClientRect(),
              y = b.getBoundingClientRect()
            const overlap =
              Math.min(x.right, y.right) - Math.max(x.left, y.left) > 1 &&
              Math.min(x.bottom, y.bottom) - Math.max(x.top, y.top) > 1
            return overlap ? [`${a.textContent?.trim()} overlaps ${b.textContent?.trim()}`] : []
          }),
        )
      })
      expect(collisions).toEqual([])
      const fonts = await page.evaluate(() =>
        [...document.fonts]
          .filter((face) =>
            ['Inter Variable', 'JetBrains Mono Variable'].includes(face.family.replaceAll('"', '')),
          )
          .map((face) => ({ family: face.family, status: face.status })),
      )
      expect(fonts.length).toBeGreaterThanOrEqual(2)
      expect(fonts.every((font) => font.status === 'loaded')).toBe(true)
      expect(await page.locator('h1').count()).toBe(1)
      expect(await page.locator('astro-island').count()).toBe(0)
      expect(await page.locator('img, canvas, video').count()).toBe(0)
      expect(await page.locator('*').count()).toBeLessThan(2500)
      expect(errors).toEqual([])
    })
  }
}
test('theme preference is shared with documentation and keyboard navigation works', async ({
  page,
}) => {
  await page.goto('/')
  const theme = await page.locator('html').getAttribute('data-theme')
  await page.locator('[data-theme-toggle]').click()
  const selected = theme === 'light' ? 'dark' : 'light'
  await expect(page.locator('html')).toHaveAttribute('data-theme', selected)
  await page.goto('/docs/')
  await expect(page.locator('html')).toHaveAttribute('data-theme', selected)
  await page.goto('/')
  await page.keyboard.press('Tab')
  await expect(page.locator('.skip-link')).toBeFocused()
  await page.keyboard.press('Enter')
  expect(await page.evaluate(() => location.hash)).toBe('#content')
  await page.setViewportSize({ width: 390, height: 844 })
  await page.locator('summary').click()
  await page.locator('.mobile-menu a[href="#your-day"]').click()
  await expect(page.locator('.mobile-menu')).not.toHaveAttribute('open', '')
})
for (const mode of ['no-js', 'reduced'] as const) {
  test(`${mode} has readable complete final states`, async ({ browser }) => {
    const page = await browser.newPage({
      javaScriptEnabled: mode !== 'no-js',
      reducedMotion: 'reduce',
    })
    await page.goto(baseURL)
    await expect(page.locator('h1')).toBeVisible()
    const offsets = await page
      .locator('[data-line-path]')
      .evaluateAll((paths) => paths.map((path) => getComputedStyle(path).strokeDashoffset))
    expect(offsets.every((value) => parseFloat(value) === 0)).toBe(true)
    await expect(page.locator('.pin-spacer')).toHaveCount(0)
    await page.close()
  })
}
test('failed enhancement bundle fails open after three seconds', async ({ page }) => {
  await page.route('**/_astro/*.js', (route) => route.abort())
  await page.goto('/')
  await expect(page.locator('html')).not.toHaveClass(/anim/, { timeout: 4500 })
  await expect(page.locator('h1')).toBeVisible()
})
test('AA contrast for all supported text pairings in both themes', async ({ page }) => {
  await page.goto('/')
  for (const theme of ['light', 'dark']) {
    const ratios = await page.evaluate((theme) => {
      document.documentElement.dataset.theme = theme
      const css = getComputedStyle(document.documentElement)
      function luminance(token: string) {
        const raw = css.getPropertyValue(token).trim().slice(1)
        const hex = raw.length === 3 ? [...raw].map((c) => c + c).join('') : raw
        const v = [0, 2, 4]
          .map((i) => parseInt(hex.slice(i, i + 2), 16) / 255)
          .map((c) => (c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4))
        return v[0]! * 0.2126 + v[1]! * 0.7152 + v[2]! * 0.0722
      }
      const pairs = ['--bg', '--surface', '--surface-2', '--blue-tint'].flatMap((bg) =>
        ['--ink', '--ink-2', '--blue-text'].map((fg) => ({ fg, bg, minimum: 4.5 })),
      )
      pairs.push(...['--diff-add', '--diff-del'].map((fg) => ({ fg, bg: '--bg', minimum: 4.5 })))
      // White on the mandated blue is 4.4988: reserved for large, bold plate labels (AA 3:1).
      pairs.push({ fg: '--on-blue', bg: '--blue-fill', minimum: 3 })
      return pairs.map((pair) => {
        const a = luminance(pair.fg),
          b = luminance(pair.bg)
        return { ...pair, ratio: (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05) }
      })
    }, theme)
    for (const pair of ratios)
      expect(pair.ratio, `${theme} ${pair.fg} on ${pair.bg}`).toBeGreaterThanOrEqual(pair.minimum)
  }
})

test('line reverses, uses cached points, and initializes distant sections lazily', async ({
  page,
}) => {
  await page.addInitScript(() => {
    const original = SVGGeometryElement.prototype.getPointAtLength
    Object.assign(window, { __pathReads: 0 })
    SVGGeometryElement.prototype.getPointAtLength = function (distance: number) {
      const counter = window as unknown as { __pathReads: number }
      counter.__pathReads++
      return original.call(this, distance)
    }
  })
  await page.setViewportSize({ width: 1536, height: 864 })
  await page.goto('/')
  await page.waitForTimeout(2000)
  await expect(page.locator('#top')).toHaveAttribute('data-initialized', 'true')
  expect(await page.locator('#extensions').getAttribute('data-initialized')).toBeNull()
  const initial = await page
    .locator('#top .desktop-line [data-line-path]')
    .evaluate((path) => parseFloat((path as SVGPathElement).style.strokeDashoffset))
  const reads = await page.evaluate(
    () => (window as unknown as { __pathReads: number }).__pathReads,
  )
  await page.evaluate(() => window.scrollTo(0, 150))
  await page.waitForTimeout(400)
  const forward = await page
    .locator('#top .desktop-line [data-line-path]')
    .evaluate((path) => parseFloat((path as SVGPathElement).style.strokeDashoffset))
  expect(forward).toBeLessThan(initial)
  expect(
    await page.evaluate(() => (window as unknown as { __pathReads: number }).__pathReads),
  ).toBe(reads)
  await page.evaluate(() => window.scrollTo(0, 0))
  await page.waitForTimeout(400)
  const reverse = await page
    .locator('#top .desktop-line [data-line-path]')
    .evaluate((path) => parseFloat((path as SVGPathElement).style.strokeDashoffset))
  expect(reverse).toBeCloseTo(initial, 2)
  await page.locator('#extensions').scrollIntoViewIfNeeded()
  await expect(page.locator('#extensions')).toHaveAttribute('data-initialized', 'true')
})

test('responsive and reduced motion contexts clean up after a viewport change', async ({
  page,
}) => {
  const errors: string[] = []
  page.on('pageerror', (error) => errors.push(error.message))
  await page.setViewportSize({ width: 1536, height: 864 })
  await page.goto('/')
  await page.waitForTimeout(1800)
  await page.setViewportSize({ width: 390, height: 844 })
  await page.waitForTimeout(1800)
  await expect(page.locator('#top .desktop-line')).not.toBeVisible()
  await expect(page.locator('#top .mobile-line')).toBeVisible()
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.waitForTimeout(250)
  const final = await page
    .locator('#top .mobile-line [data-line-path]')
    .evaluate((path) => getComputedStyle(path).strokeDashoffset)
  expect(parseFloat(final)).toBe(0)
  await expect(page.locator('#top .quadruped').last()).toHaveCSS('opacity', '1')
  expect(errors).toEqual([])
})

test('only one pen is visible while crossing section boundaries', async ({ page }) => {
  await page.setViewportSize({ width: 1536, height: 1080 })
  await page.goto('/')
  // Interrupt the load drawing with native scroll, then cross the first join.
  for (const y of [150, 480, 600, 1100]) {
    await page.evaluate((y) => window.scrollTo(0, y), y)
    await page.waitForTimeout(400)
    const pens = await page.locator('[data-pen]').evaluateAll(
      (nodes) =>
        nodes.filter((node) => {
          const rect = node.getBoundingClientRect()
          return (
            rect.width > 0 &&
            rect.bottom > 0 &&
            rect.top < innerHeight &&
            parseFloat(getComputedStyle(node).opacity) > 0
          )
        }).length,
    )
    expect(pens).toBeLessThanOrEqual(1)
  }
})

for (const width of [1024, 1280, 1536]) {
  test(`normalized non-scaling dashes match the pen at ${width}`, async ({ page }) => {
    await page.setViewportSize({ width, height: 864 })
    await page.goto('/')
    await page.waitForTimeout(1800)
    const error = await page.locator('#top .desktop-line [data-line-path]').evaluate((node) => {
      const path = node as SVGPathElement
      const scale = Math.hypot(path.getScreenCTM()!.a, path.getScreenCTM()!.b)
      const dash = parseFloat(path.style.strokeDasharray)
      // A unit path dash must equal the complete length in screen space.
      return Math.abs(dash - scale) * path.getTotalLength()
    })
    expect(error).toBeLessThan(1)
  })
}

test('the line is born exactly at the sword tip at responsive widths', async ({ page }) => {
  for (const width of [360, 390, 768, 1280, 1536]) {
    await page.setViewportSize({ width, height: 864 })
    await page.goto('/')
    await page.waitForSelector('#top[data-initialized]')
    const error = await page.evaluate(() => {
      const svg = document.querySelector<SVGSVGElement>('#top .wordmark-sword')!
      const rect = svg.getBoundingClientRect()
      const path = document.querySelector<SVGPathElement>(
        innerWidth >= 1024
          ? '#top .desktop-line [data-line-path]'
          : '#top .mobile-line [data-line-path]',
      )!
      const p = path.getPointAtLength(0)
      const screen = new DOMPoint(p.x, p.y).matrixTransform(path.getScreenCTM()!)
      return Math.hypot(
        screen.x - rect.left - rect.width / 2,
        screen.y - rect.top - (rect.height * 15) / 16,
      )
    })
    expect(error).toBeLessThanOrEqual(1)
  }
})
