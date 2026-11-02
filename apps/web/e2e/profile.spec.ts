import { test, expect } from './fixtures'

test('real profile name and initials appear in the sidebar and profile dialog', async ({
  page,
}) => {
  await page.goto('/agents/excalibur?view=chat')
  const profile = page.locator('.sidebar').getByRole('button', { name: 'Perfil', exact: true })
  await expect(profile).toContainText('Ana Example')
  await expect(profile).toContainText('AE')
  await profile.click()
  const dialog = page.getByRole('dialog', { name: 'Perfil', exact: true })
  await expect(dialog.getByText('Nombre', { exact: true })).toBeVisible()
  await expect(dialog.getByText('Ana Example', { exact: true })).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(dialog).toHaveCount(0)
  await expect(profile).toBeFocused()
})

test('loading skeleton never presents a mock identity', async ({ page }) => {
  let release!: () => void
  const pending = new Promise<void>((resolve) => {
    release = resolve
  })
  await page.route('**/rpc', async (route) => {
    if (route.request().postDataJSON().method !== 'principal.get') return route.fallback()
    await pending
    await route.fulfill({
      json: {
        jsonrpc: '2.0',
        id: route.request().postDataJSON().id,
        result: { id: 'local', name: 'Ana Prueba', timezone: 'UTC', language: 'es' },
      },
    })
  })
  await page.goto('/agents/excalibur?view=chat')
  await expect(page.locator('.sidebar .profile-skeleton')).toBeVisible()
  await expect(page.getByText('Mauricio P.', { exact: true })).toHaveCount(0)
  await page.locator('.sidebar').getByRole('button', { name: 'Perfil', exact: true }).click()
  await expect(page.getByRole('status', { name: 'Cargando perfil' })).toBeVisible()
  release()
  await expect(page.getByRole('dialog').getByText('Ana Prueba', { exact: true })).toBeVisible()
})

test('server error shows a neutral identity and an explicit retry restores the profile', async ({
  page,
}) => {
  let attempts = 0
  await page.route('**/rpc', async (route) => {
    if (route.request().postDataJSON().method !== 'principal.get') return route.fallback()
    attempts++
    const id = route.request().postDataJSON().id
    await route.fulfill({
      json:
        attempts === 1
          ? { jsonrpc: '2.0', id, error: { code: -32603, message: 'Profile unavailable' } }
          : {
              jsonrpc: '2.0',
              id,
              result: { id: 'local', name: 'Ana Prueba', timezone: 'UTC', language: 'es' },
            },
    })
  })
  await page.goto('/agents/excalibur?view=chat')
  const profile = page.locator('.sidebar').getByRole('button', { name: 'Perfil', exact: true })
  await expect(profile).toContainText('Usuario')
  await profile.click()
  await expect(page.getByText('Sin conexión con el agente', { exact: true })).toBeVisible()
  expect(attempts).toBe(1)
  await page.getByRole('dialog').getByRole('button', { name: 'Reintentar', exact: true }).click()
  await expect(profile).toContainText('Ana Prueba')
  await expect(page.getByRole('dialog').getByText('Ana Prueba', { exact: true })).toBeVisible()
  expect(attempts).toBe(2)
})
