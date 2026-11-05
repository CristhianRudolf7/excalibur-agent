import { test, expect } from './fixtures'

const navigation = [
  'Mis tareas',
  'Recordatorios',
  'Proyectos',
  'Automatizaciones',
  'Extensiones',
  'Aula',
  'Laboratorio robótico',
  'Brazo robótico',
  'Configuración',
]

for (const mode of ['desktop', 'rail', 'mobile'] as const) {
  test(`sidebar order and extension grouping in ${mode}`, async ({ page }) => {
    await page.setViewportSize({ width: mode === 'mobile' ? 390 : 1440, height: 900 })
    await page.goto('/extensions')
    await expect(page.getByRole('heading', { name: 'Extensiones', exact: true })).toBeVisible()
    if (mode === 'rail') await page.keyboard.press('Control+b')
    if (mode === 'mobile') await page.locator('.page-sidebar-toggle button').click()
    const sidebar = page.locator(mode === 'rail' ? '.sidebar-rail' : '.sidebar')
    await expect(sidebar).toBeVisible()
    const buttons = sidebar.locator('nav:not(.agent-nav):not(.sidebar-rail-agents) button')
    await expect
      .poll(async () =>
        buttons.evaluateAll((nodes) =>
          nodes.map((node) => node.getAttribute('aria-label') ?? node.textContent?.trim()),
        ),
      )
      .toEqual(
        mode === 'rail'
          ? [...navigation.slice(0, -1), 'Mis datos', 'Configuración']
          : [
              ...navigation.slice(0, -1),
              'Mis datos',
              'Ocultar colecciones',
              'Pídele al agente que cree una colección',
              'Configuración',
            ],
      )
    await expect(sidebar.getByRole('button', { name: 'Extensiones', exact: true })).toHaveAttribute(
      'aria-current',
      'page',
    )
    const installed = sidebar.getByRole('group', { name: 'Instaladas', exact: true })
    await expect(installed.getByRole('button')).toHaveCount(3)
    await expect(sidebar.getByRole('button', { name: 'Herramientas', exact: true })).toHaveCount(0)
    await expect(sidebar.getByRole('button', { name: 'Tiendas', exact: true })).toHaveCount(0)
    for (const node of await buttons.all()) await expect(node).toBeInViewport()
    if (mode === 'mobile') await page.keyboard.press('Escape')
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  })
}

test('search, categories, installation persistence and opening an extension', async ({ page }) => {
  await page.goto('/coming-soon/stores')
  await expect(page).toHaveURL(/\/extensions$/)
  const sidebar = page.locator('.sidebar')
  const cards = page.getByRole('article')
  await expect(cards).toHaveCount(3)
  const search = page.getByRole('searchbox', { name: 'Buscar extensiones' })
  await search.fill('robotico')
  await expect(cards).toHaveCount(2)
  await page.getByRole('button', { name: 'Educación', exact: true }).click()
  await expect(cards).toHaveCount(0)
  await page.getByRole('button', { name: 'Limpiar filtros', exact: true }).click()
  await expect(cards).toHaveCount(3)
  await search.fill('Aula')
  await expect(cards).toHaveCount(1)
  await expect(cards).toHaveAccessibleName('Aula')
  await search.clear()
  await page.getByRole('button', { name: 'Disponibles 0', exact: true }).click()
  await expect(page.getByText(/excalibur-extension.yaml/)).toBeVisible()
  await expect(cards).toHaveCount(0)
  await page.getByRole('button', { name: 'Instaladas 3', exact: true }).click()
  const arm = page.getByRole('article', { name: 'Brazo robótico', exact: true })
  await arm.getByRole('button', { name: 'Desinstalar', exact: true }).click()
  await expect(sidebar.getByRole('button', { name: 'Brazo robótico', exact: true })).toHaveCount(0)
  await expect(cards).toHaveCount(2)
  await page.reload()
  await expect(cards).toHaveCount(2)
  await expect(sidebar.getByRole('button', { name: 'Brazo robótico', exact: true })).toHaveCount(0)
  await page.keyboard.press('Control+b')
  const rail = page.locator('.sidebar-rail')
  await expect(rail).toBeVisible()
  await expect(rail.getByRole('button', { name: 'Brazo robótico', exact: true })).toHaveCount(0)
  await page.getByRole('button', { name: 'Disponibles 1', exact: true }).click()
  await arm.getByRole('button', { name: 'Instalar', exact: true }).click()
  await expect(rail.getByRole('button', { name: 'Brazo robótico', exact: true })).toBeVisible()
  await page.reload()
  await expect(rail.getByRole('button', { name: 'Brazo robótico', exact: true })).toBeVisible()
  await expect(arm).toBeVisible()
  await page
    .getByRole('article', { name: 'Aula', exact: true })
    .getByRole('button', { name: 'Abrir', exact: true })
    .click()
  await expect(page).toHaveURL(/\/classroom$/)
  await expect(rail.getByRole('button', { name: 'Aula', exact: true })).toHaveAttribute(
    'aria-current',
    'page',
  )
})

test('English catalog fits mobile and respects reduced motion', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.addInitScript(() => localStorage.setItem('excalibur-ui-language', 'en'))
  await page.goto('/extensions')
  await expect(page.getByRole('heading', { level: 1, name: 'Extensions' })).toBeVisible()
  await expect(page.getByRole('article', { name: 'Robot arm', exact: true })).toBeVisible()
  await expect(page.locator('.extension-card').first()).toHaveCSS('animation-name', 'none')
  expect(
    await page
      .locator('.extensions-hero')
      .evaluate((node) => getComputedStyle(node, '::before').animationName),
  ).toBe('none')
  await page.getByRole('searchbox', { name: 'Search extensions' }).fill('robot arm')
  await expect(page.getByRole('article')).toHaveCount(1)
  await page.getByRole('button', { name: 'Productivity', exact: true }).click()
  await expect(
    page.getByRole('heading', { name: 'No extensions found', exact: true }),
  ).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
})
