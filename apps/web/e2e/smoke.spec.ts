import { expect, test } from './fixtures'

test('app shell renders', async ({ page }) => {
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Excalibur', exact: true })).toBeVisible()
})

for (const viewport of [
  { width: 1440, height: 900 },
  { width: 390, height: 844 },
]) {
  test(`electric blue shell, extensions and profile fit at ${viewport.width}px`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize(viewport)
    await page.emulateMedia({ colorScheme: 'light' })
    await page.goto('/coming-soon/stores')
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('Extensiones')
    await expect(page.getByRole('heading', { name: 'Laboratorio robótico' })).toBeVisible()
    await expect(page.locator('.main-panel')).toHaveCSS('background-color', 'rgb(255, 255, 255)')
    const mobile = viewport.width < 1024
    const toggle = page.locator('.page-sidebar-toggle button')
    if (mobile) await toggle.click()
    const sidebar = page.locator('.sidebar')
    await expect(sidebar).toHaveCSS('background-color', 'rgb(255, 255, 255)')
    const stores = sidebar.getByRole('button', { name: 'Extensiones', exact: true })
    await expect(stores).toHaveAttribute('aria-current', 'page')
    await expect(stores).toHaveCSS('height', '36px')
    await expect
      .poll(async () => {
        const row = (await stores.boundingBox())!
        const bar = (await sidebar.locator('.sidebar-active-bar').boundingBox())!
        return Math.abs(row.y - bar.y)
      })
      .toBeLessThan(1)
    await page.screenshot({ path: testInfo.outputPath('shell-light.png') })
    await sidebar.getByRole('button', { name: 'Automatizaciones', exact: true }).click()
    await expect(page.getByRole('heading', { name: 'Automatizaciones', exact: true })).toBeVisible()
    await expect(page).toHaveURL(/\/automations$/)
    if (mobile) await toggle.click()
    const profile = sidebar.getByRole('button', { name: 'Perfil', exact: true })
    await profile.click()
    await expect(page.getByRole('dialog')).toContainText('Ana Example')
    await page.screenshot({ path: testInfo.outputPath('profile-light.png') })
    await page.keyboard.press('Escape')
    await expect(profile).toBeFocused()
    if (mobile) await page.keyboard.press('Escape')
    await page.screenshot({ path: testInfo.outputPath('placeholder-light.png') })
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    await page.goto('/agents/excalibur?view=chat')
    await expect(page.locator('.chat-header h1')).toHaveText('Excalibur')
    await page.screenshot({ path: testInfo.outputPath('chat-light.png') })
    if (!mobile) {
      await page.keyboard.press('Control+b')
      await expect(page.locator('.sidebar-rail')).toBeVisible()
      await page.screenshot({ path: testInfo.outputPath('rail-light.png') })
    }
  })
}

test('shell respects reduced motion and dark surface tokens', async ({ page }, testInfo) => {
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.goto('/coming-soon/tools')
  await page.evaluate(() => document.documentElement.classList.add('dark'))
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Extensiones')
  expect(
    await page
      .locator('.extensions-hero')
      .evaluate((node) => getComputedStyle(node, '::before').animationName),
  ).toBe('none')
  await expect(page.locator('.sidebar-active-bar')).toHaveCSS('transition-duration', '0s')
  expect(
    await page.locator('.main-panel').evaluate((node) => {
      const probe = document.createElement('span')
      probe.style.backgroundColor = 'var(--surface)'
      node.append(probe)
      const matches =
        getComputedStyle(node).backgroundColor === getComputedStyle(probe).backgroundColor
      probe.remove()
      return matches
    }),
  ).toBe(true)
  await page
    .locator('.sidebar')
    .getByRole('button', { name: 'Automatizaciones', exact: true })
    .click()
  await expect(page.getByRole('heading', { name: 'Automatizaciones', exact: true })).toBeVisible()
  await expect(page).toHaveURL(/\/automations$/)
  expect(await page.locator('.main-panel').evaluate((node) => node.getAnimations().length)).toBe(0)
  await page.locator('.sidebar').getByRole('button', { name: 'Perfil', exact: true }).click()
  await expect(page.getByRole('dialog')).toHaveCSS('animation-name', 'none')
  await page.screenshot({ path: testInfo.outputPath('shell-dark.png') })
})
