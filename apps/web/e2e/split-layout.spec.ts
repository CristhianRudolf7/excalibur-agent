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

async function roundTrip(page: Page, shortcut: boolean) {
  if (shortcut) await page.keyboard.press('Control+j')
  else
    await page
      .getByRole('group', { name: 'Modo de vista' })
      .getByRole('button', { name: 'Mundo 2D', exact: true })
      .click()
  await expect(page.locator('.world-canvas')).toHaveAttribute('data-world-ready', 'true', {
    timeout: 30_000,
  })
  if (shortcut) await page.keyboard.press('Control+j')
  else
    await page
      .getByRole('group', { name: 'Modo de vista' })
      .getByRole('button', { name: 'Chat', exact: true })
      .click()
  await expect(page.locator('.chat-header')).toBeVisible()
}

for (const viewport of [
  { width: 1280, height: 800 },
  { width: 1536, height: 1024 },
]) {
  test(`split survives repeated mode switches, resizing and sidebar changes at ${viewport.width}px`, async ({
    page,
  }) => {
    await page.setViewportSize(viewport)
    const errors: string[] = []
    page.on('pageerror', (error) => errors.push(error.message))
    await page.goto('/agents/excalibur?panel=files&view=chat')
    await page.getByRole('button', { name: /Alternar panel|Toggle panel/ }).click()
    const initial = await splitBoxes(page)
    for (let i = 0; i < 3; i++) {
      await roundTrip(page, i % 2 === 0)
      const current = await splitBoxes(page)
      expect(Math.abs(current.chat.width - initial.chat.width)).toBeLessThanOrEqual(4)
      expect(Math.abs(current.panel.width - initial.panel.width)).toBeLessThanOrEqual(4)
      await expect(page.locator('.sidebar')).not.toHaveClass(/sidebar-hidden/)
      await expect(page.locator('.app')).not.toHaveClass(/world-mode/)
    }
    const handle = page.getByRole('separator', { name: 'Cambiar ancho del panel', exact: true })
    const bounds = (await handle.boundingBox())!
    await page.mouse.move(bounds.x, bounds.y + 100)
    await page.mouse.down()
    await page.mouse.move(bounds.x - 90, bounds.y + 100, { steps: 8 })
    await page.mouse.up()
    const custom = await splitBoxes(page)
    expect(Math.abs(custom.chat.width - initial.chat.width)).toBeGreaterThan(60)
    await roundTrip(page, false)
    expect(Math.abs((await splitBoxes(page)).chat.width - custom.chat.width)).toBeLessThanOrEqual(4)
    await page.reload()
    await page.getByRole('button', { name: 'Alternar panel', exact: true }).click()
    expect(Math.abs((await splitBoxes(page)).chat.width - custom.chat.width)).toBeLessThanOrEqual(4)
    await page.keyboard.press('Control+b')
    const expanded = await splitBoxes(page)
    await roundTrip(page, true)
    await expect(page.locator('.sidebar')).toHaveClass(/sidebar-hidden/)
    expect(Math.abs((await splitBoxes(page)).chat.width - expanded.chat.width)).toBeLessThanOrEqual(
      4,
    )
    expect(errors).toEqual([])
  })
}

test('tablet chat stays visible and fits after mode round trips at 1000px', async ({ browser }) => {
  const context = await browser.newContext({
    viewport: { width: 1000, height: 620 },
    hasTouch: true,
    isMobile: true,
  })
  const page = await context.newPage()
  await mockProfile(page)
  try {
    await page.goto('/agents/excalibur?panel=files&view=chat')
    for (const shortcut of [false, true, false]) {
      await roundTrip(page, shortcut)
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
