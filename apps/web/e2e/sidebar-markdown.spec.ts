import { mockChat } from './chat-fixture'
import { markdownMessages } from './markdown-fixtures'
import { test, expect, type Page } from './fixtures'

async function noHorizontalOverflow(page: Page) {
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  expect(
    await page.locator('.chat-column').evaluate((node) => node.scrollWidth <= node.clientWidth),
  ).toBe(true)
}
async function splitFits(page: Page) {
  await expect
    .poll(async () => {
      const main = (await page.locator('.main-panel').boundingBox())!
      const chat = (await page.locator('.chat-column').boundingBox())!
      const panel = (await page.locator('.right-panel').boundingBox())!
      const handle = (await page
        .getByRole('separator', { name: 'Cambiar ancho del panel', exact: true })
        .boundingBox())!
      return (
        chat.width >= 360 &&
        panel.width >= 320 &&
        Math.abs(chat.width + panel.width + handle.width - main.width) <= 4
      )
    })
    .toBe(true)
}
test('sidebar uses shared resizing, stays within bounds and restores its custom width', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 })
  await page.goto('/agents/excalibur?view=chat&worldDebug=1&worldClock=manual')
  await page.getByRole('button', { name: 'Alternar panel', exact: true }).click()
  const sidebar = page.locator('.sidebar')
  const handle = page.getByRole('separator', { name: 'Redimensionar barra lateral', exact: true })
  await expect(sidebar).toHaveCSS('width', '240px')
  await expect(handle).toHaveAttribute('aria-valuemin', '200')
  await expect(handle).toHaveAttribute('aria-valuemax', '400')
  const box = (await handle.boundingBox())!
  await page.mouse.move(box.x + box.width / 2, box.y + 100)
  await page.mouse.down()
  await page.mouse.move(520, box.y + 100, { steps: 8 })
  await page.mouse.up()
  await expect(sidebar).toHaveCSS('width', '400px')
  await splitFits(page)
  await handle.focus()
  await page.keyboard.press('Home')
  await expect(sidebar).toHaveCSS('width', '200px')
  for (const node of await sidebar
    .locator('.online-dot, .working-spinner, .unread, .nav-count')
    .all())
    await expect(node).toBeInViewport()
  await page.keyboard.press('ArrowRight')
  await expect(sidebar).toHaveCSS('width', '224px')
  await page.keyboard.press('End')
  await expect(sidebar).toHaveCSS('width', '400px')
  await handle.dblclick()
  await expect(sidebar).toHaveCSS('width', '240px')
  const reset = (await handle.boundingBox())!
  await page.mouse.move(reset.x + reset.width / 2, reset.y + 100)
  await page.mouse.down()
  await page.mouse.move(320, reset.y + 100, { steps: 5 })
  await page.mouse.up()
  await expect(sidebar).toHaveCSS('width', '320px')
  await splitFits(page)
  await page.reload()
  await page.getByRole('button', { name: 'Alternar panel', exact: true }).click()
  await expect(sidebar).toHaveCSS('width', '320px')
  await page.keyboard.press('Control+b')
  await expect(sidebar).toHaveCSS('width', '0px')
  await expect(page.locator('.sidebar-slot')).toHaveCSS('width', '64px')
  await expect(page.locator('.sidebar-rail')).toBeVisible()
  await expect(handle).toHaveCount(0)
  await page.keyboard.press('Control+b')
  await expect(sidebar).toHaveCSS('width', '320px')
  await page.keyboard.press('Control+j')
  await expect(page.locator('.world-canvas')).toHaveAttribute('data-world-ready', 'true')
  await expect(handle).toHaveCount(0)
  await page.keyboard.press('Control+j')
  await expect(sidebar).toHaveCSS('width', '320px')
  await splitFits(page)
  await noHorizontalOverflow(page)
})

test('mobile drawer has a fixed width and no sidebar resize handle', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await page.addInitScript(() => localStorage.setItem('sidebar-width-v1', '380'))
  await page.goto('/agents/excalibur?view=chat')
  await expect(
    page.getByRole('separator', { name: 'Redimensionar barra lateral', exact: true }),
  ).toHaveCount(0)
  await page.getByRole('button', { name: 'Alternar barra lateral', exact: true }).last().click()
  await expect(page.locator('.sidebar')).toHaveCSS('width', '260px')
  await noHorizontalOverflow(page)
})

for (const width of [1536, 390]) {
  test(`Markdown tables and code scroll inside messages at ${width}px and copy works`, async ({
    page,
    context,
  }) => {
    await context.grantPermissions(['clipboard-read', 'clipboard-write'])
    await page.setViewportSize({ width, height: 844 })
    await mockChat(page, [{ text: markdownMessages.markdownVoice[0] }])
    await page.goto('/agents/excalibur?view=chat')
    if (width >= 1024)
      await page.getByRole('button', { name: 'Alternar panel', exact: true }).click()
    const table = page.locator('.markdown table')
    await expect(table.getByRole('columnheader', { name: 'Modelo', exact: true })).toBeVisible()
    await expect(table.locator('tbody tr')).toHaveCount(5)
    await expect(table.getByRole('columnheader', { name: 'Latencia (ms)' })).toHaveCSS(
      'text-align',
      'right',
    )
    const scroll = page.locator('.markdown-table-scroll')
    await scroll.scrollIntoViewIfNeeded()
    await expect
      .poll(() => scroll.evaluate((node) => node.scrollWidth > node.clientWidth))
      .toBe(true)
    await scroll.evaluate((node) => {
      node.scrollLeft = node.scrollWidth
    })
    expect(await scroll.evaluate((node) => node.scrollLeft)).toBeGreaterThan(0)
    await noHorizontalOverflow(page)
    const code = page.getByLabel('Código json', { exact: true })
    await code.scrollIntoViewIfNeeded()
    await expect.poll(() => code.evaluate((node) => node.scrollWidth > node.clientWidth)).toBe(true)
    await code.evaluate((node) => {
      node.scrollLeft = node.scrollWidth
    })
    expect(await code.evaluate((node) => node.scrollLeft)).toBeGreaterThan(0)
    const block = page.locator('.markdown-code-block').filter({ has: code })
    await expect(block.locator('.hljs-attr').first()).toBeVisible()
    const source = await code.textContent()
    await block.getByRole('button', { name: 'Copiar', exact: true }).click()
    await expect(block.getByRole('button', { name: 'Copiado', exact: true })).toBeVisible()
    expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(source)
    await expect(block.getByRole('button', { name: 'Copiar', exact: true })).toBeVisible()
    await noHorizontalOverflow(page)
  })
}

test('live code and budget replies are rich Markdown while user messages remain plain text', async ({
  page,
}) => {
  await mockChat(page, [{ text: markdownMessages.markdownByte[0] }])
  await page.goto('/agents/excalibur?view=chat')
  await expect(page.getByLabel('Código typescript', { exact: true })).toContainText(
    'interface Session',
  )
  await mockChat(page, [{ text: markdownMessages.markdownAtlas[0] }])
  await page.goto('/agents/excalibur?view=chat')
  await expect(page.locator('.markdown table')).toContainText('1.856,25 €')
  await expect(page.locator('.markdown table')).toContainText('5.237,00 €')
  const input = page.getByRole('textbox', { name: 'Escribe a Excalibur…' })
  await input.fill('**Keep this plain**\n<script>literal</script>')
  await input.press('Enter')
  const user = page.locator('.message-user').last()
  await expect(user).toContainText('**Keep this plain**')
  await expect(user).toContainText('<script>literal</script>')
  await expect(user.locator('strong, script')).toHaveCount(0)
  await expect(user.locator('p')).toHaveCSS('white-space', 'pre-wrap')
})
