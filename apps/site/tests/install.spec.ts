import { expect, test } from './browser'
import { baseURL } from './server'

const commands = [
  'curl -fsSL https://excalibur.crehlabs.com/install.sh | sh',
  'curl -fsSL https://excalibur.crehlabs.com/install.sh | sh',
  'irm https://excalibur.crehlabs.com/install.ps1 | iex',
  'docker run -d --name excalibur -p 127.0.0.1:7787:7787 -v excalibur-data:/data ghcr.io/cristhianrudolf7/excalibur:latest',
]

test('install tabs select the OS, support keyboard navigation, and copy exact commands', async ({
  browser,
}) => {
  const context = await browser.newContext({
    permissions: ['clipboard-read', 'clipboard-write'],
    userAgent: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/142.0.0.0',
  })
  const page = await context.newPage()
  await page.goto(`${baseURL}/#install`)
  const tabs = page.locator('[data-install-tab]')
  await expect(tabs.nth(2)).toHaveAttribute('aria-selected', 'true')
  await tabs.nth(2).focus()
  await page.keyboard.press('ArrowRight')
  await expect(tabs.nth(3)).toBeFocused()
  await expect(tabs.nth(3)).toHaveAttribute('aria-selected', 'true')
  await page.getByRole('button', { name: 'Copy command', exact: true }).click()
  await expect(page.locator('[data-install-status]')).toHaveText('Command copied.')
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(commands[3])
  await tabs.nth(3).focus()
  await page.keyboard.press('Home')
  await expect(tabs.first()).toHaveAttribute('aria-selected', 'true')
  await page.keyboard.press('End')
  await expect(tabs.last()).toHaveAttribute('aria-selected', 'true')
  await page.keyboard.press('ArrowRight')
  await expect(tabs.first()).toBeFocused()
  await context.close()
})

for (const width of [360, 390, 1024, 1536]) {
  test(`install is complete without JavaScript at ${width}`, async ({ browser }) => {
    const page = await browser.newPage({
      javaScriptEnabled: false,
      viewport: { width, height: 844 },
    })
    await page.goto(`${baseURL}/#install`)
    const panels = page.locator('[data-install-panel]')
    for (let i = 0; i < commands.length; i++) {
      await expect(panels.nth(i)).toBeVisible()
      await expect(panels.nth(i).locator('code')).toHaveText(commands[i]!)
    }
    await expect(page.locator('[data-install-copy]')).toBeHidden()
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    const overlap = await page.locator('#install').evaluate((section) => {
      const terminal = section.querySelector('.install-terminal')!.getBoundingClientRect()
      const link = section.querySelector('.install-instructions')!.getBoundingClientRect()
      return Math.min(terminal.bottom, link.bottom) - Math.max(terminal.top, link.top)
    })
    expect(overlap).toBeLessThanOrEqual(0)
    await page.close()
  })
}

test('numbering has eight sections and a separate machine interlude', async ({ page }) => {
  await page.goto('/')
  const indexes = await page.locator('.section-index').allTextContents()
  expect(indexes.map((text) => text.trim())).toEqual(
    Array.from({ length: 8 }, (_, i) => `0${i + 1} / 08`),
  )
  expect(
    await page
      .locator('main > section, main > .machine-track')
      .evaluateAll((nodes) => nodes.map((node) => node.id || 'machine')),
  ).toEqual([
    'top',
    'your-day',
    'memory',
    'data',
    'code',
    'work',
    'machine',
    'measured',
    'install',
    'extensions',
  ])
})

test('history restores motion and theme after returning from documentation', async ({ page }) => {
  await page.goto('/#install')
  await expect(page.locator('#install')).toHaveAttribute('data-initialized', 'true')
  await page.goto('/docs/getting-started/install/')
  await page.goBack()
  await expect(page.locator('#install')).toHaveAttribute('data-initialized', 'true')
  await page.locator('[data-install-tab]').last().click()
  await expect(page.locator('[data-install-tab]').last()).toHaveAttribute('aria-selected', 'true')
  await page.setViewportSize({ width: 390, height: 844 })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await expect(page.locator('#install .mobile-line')).toBeVisible()
  await expect(page.locator('#install .desktop-line')).toBeHidden()
})

for (const width of [360, 390]) {
  test(`static sword meets the rail without JavaScript at ${width}`, async ({ browser }) => {
    const page = await browser.newPage({
      javaScriptEnabled: false,
      viewport: { width, height: 844 },
    })
    await page.goto(baseURL)
    await page.evaluate(() => document.fonts.ready)
    const gap = await page.evaluate(() => {
      const source = document.querySelector('.wordmark-sword')!.getBoundingClientRect()
      const path = document.querySelector<SVGPathElement>('.hero .mobile-line [data-line-path]')!
      const p = path.getPointAtLength(0)
      const point = new DOMPoint(p.x, p.y).matrixTransform(path.getScreenCTM()!)
      return Math.hypot(
        point.x - source.x - source.width / 2,
        point.y - source.y - (source.height * 15) / 16,
      )
    })
    expect(gap).toBeLessThanOrEqual(1)
    await page.close()
  })
}

test('the shared pen remains visible at exact section joins in both scroll directions', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 720 })
  await page.goto('/')
  for (const section of ['memory', 'install']) {
    const join = await page
      .locator(`#${section}`)
      .evaluate((el) => el.getBoundingClientRect().top + scrollY - innerHeight / 2)
    for (const side of [-1, 1]) {
      await page.evaluate((y) => scrollTo(0, y), join + side * 80)
      await page.waitForTimeout(400)
      await page.evaluate((y) => scrollTo(0, y), join)
      await page.waitForTimeout(400)
      const visible = await page.locator('[data-pen]').evaluateAll(
        (nodes) =>
          nodes.filter((node) => {
            const r = node.getBoundingClientRect()
            return (
              r.width > 0 &&
              r.bottom > 0 &&
              r.top < innerHeight &&
              parseFloat(getComputedStyle(node).opacity) > 0
            )
          }).length,
      )
      expect(visible).toBe(1)
    }
  }
})
