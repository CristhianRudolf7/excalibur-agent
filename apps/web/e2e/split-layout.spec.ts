import { expect, test, mockProfile, type Page } from './fixtures'

async function splitBoxes(page: Page) {
  const sidebar = page.locator('.sidebar')
  await expect(sidebar).toHaveCSS(
    'width',
    (await sidebar.getAttribute('class'))?.includes('sidebar-hidden') ? '0px' : '240px',
  )
  const collapsed = (await sidebar.getAttribute('class'))?.includes('sidebar-hidden')
  await expect(page.locator('.sidebar-slot')).toHaveCSS('width', collapsed ? '64px' : '241px')
  if (collapsed) await expect(page.locator('.sidebar-rail')).toBeVisible()
  const chat = (await page.locator('.chat-column').boundingBox())!
  const panel = (await page.locator('.right-panel').boundingBox())!
  const separator = (await page
    .getByRole('separator', { name: 'Cambiar ancho del panel', exact: true })
    .boundingBox())!
  const main = (await page.locator('.main-panel').boundingBox())!
  expect(chat.width).toBeGreaterThanOrEqual(360)
  expect(panel.width).toBeGreaterThanOrEqual(320)
  expect(panel.x).toBeGreaterThan(chat.x)
  expect(Math.abs(chat.width + panel.width + separator.width - main.width)).toBeLessThanOrEqual(4)
  return { chat, panel }
}

for (const viewport of [
  { width: 1280, height: 800 },
  { width: 1536, height: 1024 },
]) {
  test(`split preserves resizing and sidebar changes at ${viewport.width}px`, async ({ page }) => {
    await page.setViewportSize(viewport)
    const errors: string[] = []
    page.on('pageerror', (error) => errors.push(error.message))
    await page.goto('/agents/excalibur?panelDeepLink=1&panelDeepLink=1&panel=files&view=chat')
    await page.getByRole('button', { name: /Alternar panel|Toggle panel/ }).click()
    const initial = await splitBoxes(page)
    const handle = page.getByRole('separator', { name: 'Cambiar ancho del panel', exact: true })
    const bounds = (await handle.boundingBox())!
    await page.mouse.move(bounds.x, bounds.y + 100)
    await page.mouse.down()
    await page.mouse.move(bounds.x - 90, bounds.y + 100, { steps: 8 })
    await page.mouse.up()
    const custom = await splitBoxes(page)
    expect(Math.abs(custom.chat.width - initial.chat.width)).toBeGreaterThan(60)
    expect(Math.abs((await splitBoxes(page)).chat.width - custom.chat.width)).toBeLessThanOrEqual(4)
    await page.reload()
    await page.getByRole('button', { name: 'Alternar panel', exact: true }).click()
    expect(Math.abs((await splitBoxes(page)).chat.width - custom.chat.width)).toBeLessThanOrEqual(4)
    await page.keyboard.press('Control+b')
    const expanded = await splitBoxes(page)
    await expect(page.locator('.sidebar')).toHaveClass(/sidebar-hidden/)
    expect(Math.abs((await splitBoxes(page)).chat.width - expanded.chat.width)).toBeLessThanOrEqual(
      4,
    )
    expect(errors).toEqual([])
  })
}

test('tablet chat stays visible and fits at 1000px', async ({ browser }) => {
  const context = await browser.newContext({
    viewport: { width: 1000, height: 620 },
    hasTouch: true,
    isMobile: true,
  })
  const page = await context.newPage()
  await mockProfile(page)
  try {
    await page.goto('/agents/excalibur?panelDeepLink=1&panelDeepLink=1&panel=files&view=chat')
    {
      await expect(page.locator('.chat-column')).toBeVisible()
      expect((await page.locator('.chat-column').boundingBox())!.width).toBeGreaterThanOrEqual(360)
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
        true,
      )
    }
  } finally {
    await context.close()
  }
})
