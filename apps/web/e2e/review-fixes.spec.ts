import { mockChat } from './chat-fixture'
import { expect, test } from './fixtures'

test('budget fits A–H and reveals the active cell on load, navigation and resize', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1536, height: 1024 })
  await page.goto('/agents/atlas?panel=sheet')
  await page.getByRole('button', { name: /Alternar panel|Toggle panel/ }).click()
  const cell = (address: string) =>
    page.getByRole('gridcell', { name: `Celda ${address}`, exact: true })
  await expect(cell('H4')).toHaveAttribute('aria-selected', 'true')
  const insideSheet = async (address: string) => {
    const cellBox = await cell(address).boundingBox()
    const viewport = await page.locator('.sheet-viewport').boundingBox()
    return (
      !!cellBox &&
      !!viewport &&
      cellBox.x >= viewport.x + 37 &&
      cellBox.x + cellBox.width <= viewport.x + viewport.width + 1 &&
      cellBox.y >= viewport.y + 28 &&
      cellBox.y + cellBox.height <= viewport.y + viewport.height
    )
  }
  await expect.poll(() => insideSheet('A1')).toBe(true)
  await expect.poll(() => insideSheet('H4')).toBe(true)
  await page.getByRole('textbox', { name: 'Celda activa' }).fill('Z70')
  await expect.poll(() => insideSheet('Z70')).toBe(true)
  await page.getByRole('textbox', { name: 'Fórmula', exact: true }).press('Escape')
  await expect(page.getByRole('grid', { name: 'Hoja de cálculo' })).toBeFocused()
  await page.keyboard.press('ArrowLeft')
  await expect(cell('Y70')).toHaveAttribute('aria-selected', 'true')
  await expect.poll(() => insideSheet('Y70')).toBe(true)
  await page.getByRole('textbox', { name: 'Celda activa' }).fill('H4')
  const handle = (await page
    .getByRole('separator', { name: 'Cambiar ancho del panel', exact: true })
    .boundingBox())!
  await page.mouse.move(handle.x, handle.y + 200)
  await page.mouse.down()
  await page.mouse.move(handle.x + 170, handle.y + 200, { steps: 10 })
  await page.mouse.up()
  await expect.poll(() => insideSheet('H4')).toBe(true)
})

test('live plan and question cards stay contained at 390 px', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  const chat = await mockChat(page)
  await page.goto('/agents/excalibur?panel=files')
  await page.getByRole('button', { name: /Alternar panel|Toggle panel/ }).click()
  await expect(page.locator('.chat-connection')).toHaveCount(0)
  chat.emit('plan.approval_requested', {
    plan_id: 'p1',
    summary: 'Review the implementation and verification plan',
  })
  chat.emit('question.asked', {
    question_id: 'q1',
    content: 'Which requirements should be verified?',
  })
  for (const card of await page.locator('.chat-interaction').all()) {
    await card.scrollIntoViewIfNeeded()
    expect(await card.evaluate((node) => node.scrollWidth <= node.clientWidth)).toBe(true)
    const box = (await card.boundingBox())!
    expect(box.x).toBeGreaterThanOrEqual(0)
    expect(box.x + box.width).toBeLessThanOrEqual(390)
  }
  await expect(page.locator('.chat-interaction')).toHaveCount(2)
})

for (const width of [390, 1536]) {
  test(`sent unbroken messages wrap inside the bubble at ${width} px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 844 })
    await page.goto('/agents/excalibur?panel=files')
    if (width >= 1024)
      await page.getByRole('button', { name: /Alternar panel|Toggle panel/ }).click()
    for (const text of ['x'.repeat(400), `https://example.com/${'long-path'.repeat(60)}`]) {
      const composer = page.getByRole('textbox', { name: 'Escribe a Excalibur…' })
      await composer.fill(text)
      await composer.press('Enter')
      const bubble = page.locator('.message-user p').filter({ hasText: text })
      await expect(bubble).toHaveCount(1)
      await expect(bubble).toBeVisible()
      expect(await bubble.evaluate((node) => node.scrollWidth <= node.clientWidth)).toBe(true)
      const bounds = (await bubble.boundingBox())!
      const chat = (await page.locator('.chat-column').boundingBox())!
      expect(bounds.x).toBeGreaterThanOrEqual(chat.x)
      expect(bounds.x + bounds.width).toBeLessThanOrEqual(chat.x + chat.width)
      expect(bounds.height).toBeGreaterThan(45)
      expect(
        await page.locator('.chat-column').evaluate((node) => node.scrollWidth <= node.clientWidth),
      ).toBe(true)
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
        true,
      )
      const stop = page.getByRole('button', { name: 'Detener respuesta' })
      if (await stop.isVisible()) await stop.click()
      else await expect(page.getByRole('button', { name: 'Enviar mensaje' })).toBeVisible()
    }
  })
}
