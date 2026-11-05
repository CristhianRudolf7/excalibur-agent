import { test, expect, type Page } from './fixtures'

async function splitFillsMain(page: Page) {
  await expect
    .poll(async () => {
      const chat = (await page.locator('.chat-column').boundingBox())!
      const panel = (await page.locator('.right-panel').boundingBox())!
      const separator = (await page
        .getByRole('separator', { name: 'Cambiar ancho del panel', exact: true })
        .boundingBox())!
      const main = (await page.locator('.main-panel').boundingBox())!
      return (
        chat.width >= 360 &&
        panel.width >= 320 &&
        Math.abs(chat.width + panel.width + separator.width - main.width) <= 4
      )
    })
    .toBe(true)
}
test('desktop collapse shows a navigation rail, preserves sizing and supports navigation and profile', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1536, height: 1024 })
  await page.goto('/agents/excalibur?view=chat')
  await page.getByRole('button', { name: 'Alternar panel', exact: true }).click()
  const sidebar = page.locator('.sidebar')
  const slot = page.locator('.sidebar-slot')
  const resize = page.getByRole('separator', { name: 'Redimensionar barra lateral', exact: true })
  const handle = (await resize.boundingBox())!
  await page.mouse.move(handle.x + handle.width / 2, handle.y + 100)
  await page.mouse.down()
  await page.mouse.move(320, handle.y + 100, { steps: 5 })
  await page.mouse.up()
  await expect(sidebar).toHaveCSS('width', '320px')
  await page
    .locator('.chat-header')
    .getByRole('button', { name: 'Alternar barra lateral', exact: true })
    .click()
  const rail = page.getByRole('complementary', { name: 'Navegación compacta' })
  await expect(rail).toBeVisible()
  await expect(slot).toHaveCSS('width', '64px')
  expect((await rail.boundingBox())!.width).toBe(64)
  await expect(sidebar).toBeHidden()
  await expect(sidebar.getByText('Bandeja de entrada', { exact: true })).toBeHidden()
  await expect(rail.locator('.sidebar-rail-agents .pixel-avatar')).toHaveCount(1)
  await expect(resize).toHaveCount(0)
  await expect(
    page
      .locator('.chat-header')
      .getByRole('button', { name: 'Alternar barra lateral', exact: true }),
  ).toHaveCount(1)
  for (const label of [
    'Mis tareas',
    'Recordatorios',
    'Proyectos',
    'Aula',
    'Brazo robótico',
    'Extensiones',
    'Laboratorio robótico',
    'Automatizaciones',
    'Configuración',
  ])
    await expect(rail.getByRole('button', { name: label, exact: true })).toBeVisible()
  await expect(rail.getByRole('button', { name: 'Métricas', exact: true })).toHaveCount(0)
  await rail.getByRole('button', { name: 'Configuración', exact: true }).focus()
  await expect(page.getByRole('tooltip', { name: 'Configuración', exact: true })).toBeVisible()
  await page.keyboard.press('Escape')
  await rail.getByRole('button', { name: 'Excalibur', exact: true }).click()
  await expect(page).toHaveURL(/agents\/excalibur/)
  await expect(page.locator('.chat-header h1')).toHaveText('Excalibur')
  await expect(rail.getByRole('button', { name: 'Excalibur', exact: true })).toHaveAttribute(
    'aria-pressed',
    'true',
  )
  await splitFillsMain(page)
  await expect(rail.getByRole('button', { name: 'Perfil', exact: true })).toHaveText('AE')
  await rail.getByRole('button', { name: 'Perfil', exact: true }).click()
  await expect(page.getByRole('dialog', { name: 'Perfil', exact: true })).toContainText(
    'Ana Example',
  )
  await page.keyboard.press('Escape')
  await page.reload()
  await page.getByRole('button', { name: 'Alternar panel', exact: true }).click()
  await expect(rail).toBeVisible()
  await expect(slot).toHaveCSS('width', '64px')
  await expect(page.locator('.chat-header')).toBeVisible()
  await expect(slot).toHaveCSS('width', '64px')
  await splitFillsMain(page)
  await page
    .locator('.chat-header')
    .getByRole('button', { name: 'Alternar barra lateral', exact: true })
    .click()
  await expect(rail).toHaveCount(0)
  await expect(sidebar).toHaveCSS('width', '320px')
  await expect(slot).toHaveCSS('width', '321px')
  await page.keyboard.press('Control+b')
  await expect(rail).toBeVisible()
  await expect(slot).toHaveCSS('width', '64px')
  await page.keyboard.press('Control+b')
  await expect(sidebar).toHaveCSS('width', '320px')
  await expect(slot).toHaveCSS('width', '321px')
  await expect(sidebar).toHaveCSS('width', '320px')
  await expect(slot).toHaveCSS('width', '321px')
  await splitFillsMain(page)
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.keyboard.press('Control+b')
  await expect(slot).toHaveCSS('transition-duration', '0s')
  await rail.getByRole('button', { name: 'Proyectos', exact: true }).click()
  await expect(page).toHaveURL(/\/projects/)
  await expect(rail.getByRole('button', { name: 'Proyectos', exact: true })).toHaveAttribute(
    'aria-pressed',
    'true',
  )
  for (const [label, path] of [
    ['Aula', '/classroom'],
    ['Brazo robótico', '/robot'],
  ]) {
    await rail.getByRole('button', { name: label, exact: true }).click()
    await expect(page).toHaveURL(new RegExp(`${path}$`))
    await expect(rail.getByRole('button', { name: label, exact: true })).toHaveAttribute(
      'aria-pressed',
      'true',
    )
    await expect(rail.getByRole('button', { name: 'Excalibur', exact: true })).toHaveAttribute(
      'aria-pressed',
      'false',
    )
  }
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
})

test('mobile chat keeps the overlay drawer with no rail', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  {
    await page.goto('/agents/excalibur?view=chat')
    await expect(page.locator('.sidebar-rail')).toHaveCount(0)
    await page
      .locator('.main-panel')
      .getByRole('button', { name: 'Alternar barra lateral', exact: true })
      .click()
    await expect(page.locator('.sidebar')).toBeVisible()
    await expect(page.locator('.sidebar')).toHaveCSS('width', '260px')
    await expect(
      page.getByRole('separator', { name: 'Redimensionar barra lateral', exact: true }),
    ).toHaveCount(0)
    await page.keyboard.press('Escape')
    await expect(page.locator('.sidebar')).toBeHidden()
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  }
})
