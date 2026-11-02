import { test, expect } from './fixtures'
import { mockAutomations, sampleRun } from './fixtures/automations'
import { mkdir } from 'node:fs/promises'
const shots = '/home/cristhian/projects/ui-design/auto-shots'
test('sidebar, cards, filters and all lifecycle actions', async ({ page }) => {
  const mock = await mockAutomations(page)
  await page.goto('/agents/excalibur?view=chat')
  await page
    .locator('.sidebar')
    .getByRole('button', { name: 'Automatizaciones', exact: true })
    .click()
  await expect(page).toHaveURL(/\/automations$/)
  await expect(page.getByRole('heading', { name: 'Automatizaciones', exact: true })).toBeVisible()
  await expect(page.locator('.auto-card')).toHaveCount(3)
  await page.getByRole('button', { name: 'Pausadas', exact: true }).click()
  await expect(page.locator('.auto-card')).toHaveCount(1)
  await page.getByRole('button', { name: 'Todas', exact: true }).click()
  const daily = page
    .locator('.auto-card')
    .filter({ has: page.getByRole('heading', { name: 'AI morning briefing' }) })
  await daily.getByRole('button', { name: 'Pausar', exact: true }).click()
  await expect(daily.getByRole('button', { name: 'Reanudar', exact: true })).toBeVisible()
  await daily.getByRole('button', { name: 'Reanudar', exact: true }).click()
  await expect(daily.getByRole('button', { name: 'Pausar', exact: true })).toBeVisible()
  await daily.getByRole('button', { name: 'Ejecutar ahora' }).click()
  await expect(daily.getByText('En ejecución', { exact: true })).toBeVisible()
  await expect(daily.getByRole('button', { name: 'Ejecutar ahora' })).toBeDisabled()
  await daily.getByRole('button', { name: 'Eliminar', exact: true }).click()
  await page.getByRole('dialog').getByRole('button', { name: 'Eliminar', exact: true }).click()
  await expect(page.locator('.auto-card')).toHaveCount(2)
  for (const method of ['run', 'pause', 'resume', 'delete'])
    expect(mock.calls.some((c) => c.method === `automations.${method}`)).toBe(true)
})
test('live runs, editing context and sending readable bubbles', async ({ page }) => {
  const { events } = await mockAutomations(page)
  await page.goto('/automations')
  await events.ready('automation.run')
  events.emit('automation.run', {
    run: { ...sampleRun, status: 'running', started_at: new Date().toISOString() },
  })
  await expect(page.getByText('En ejecución', { exact: true })).toBeVisible()
  await page
    .locator('.auto-card')
    .first()
    .getByRole('button', { name: 'Editar', exact: true })
    .click()
  await expect(page).toHaveURL(/agents\/excalibur/)
  await expect(page.locator('.auto-context-chip')).toContainText('AI morning briefing · v1')
  const composer = page.locator('.composer textarea')
  await expect(composer).toBeFocused()
  await expect(composer).toHaveAttribute('placeholder', 'Describe el cambio que quieres…')
  await composer.fill('Move to 9')
  const sent = page.waitForRequest(
    (r) => r.url().endsWith('/rpc') && r.postDataJSON().method === 'conversation.send',
  )
  await page.getByRole('button', { name: 'Enviar mensaje', exact: true }).click()
  expect((await sent).postDataJSON().params.text).toContain(
    '[Automatización adjunta: id=daily versión=1',
  )
  await expect(page.locator('.message-user')).toContainText(
    '📎 Automatización: AI morning briefing',
  )
  await expect(page.locator('.message-user')).not.toContainText('[/automation]')
})
test('empty state prefills chat without sending', async ({ page }) => {
  await mockAutomations(page, [])
  await page.goto('/automations')
  await expect(page.getByText('Aún no tienes automatizaciones')).toBeVisible()
  await page.getByRole('button', { name: 'Ir al chat' }).click()
  await expect(page.locator('.composer textarea')).toHaveValue(
    'Cada mañana a las 8 resume las noticias de IA y guárdalo en un archivo',
  )
  await expect(page.locator('.message-user')).toHaveCount(0)
})
test('slow loading has layout skeletons', async ({ page }) => {
  await mockAutomations(page, undefined, 2000)
  await page.goto('/automations')
  await expect(page.locator('.auto-skeleton')).toHaveCount(3)
  await expect(page.locator('.auto-skeleton')).toHaveCount(0)
})
test('responsive light and dark visual review', async ({ page }) => {
  await mkdir(shots, { recursive: true })
  await mockAutomations(page)
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/automations')
  await expect(page.locator('.auto-card')).toHaveCount(3)
  await page.screenshot({ animations: 'disabled', path: `${shots}/fe1-desktop.png` })
  await page.setViewportSize({ width: 390, height: 844 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  )
  await page.screenshot({ animations: 'disabled', path: `${shots}/fe1-mobile.png` })
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.evaluate(() => document.documentElement.classList.add('dark'))
  await page.screenshot({ animations: 'disabled', path: `${shots}/fe1-dark.png` })
})
test('detail route and deletion events leave the removed detail', async ({ page }) => {
  const { events } = await mockAutomations(page)
  await page.goto('/automations')
  await page.getByRole('link', { name: 'AI morning briefing', exact: true }).click()
  await expect(page).toHaveURL(/\/automations\/daily$/)
  await expect(page.getByRole('heading', { name: 'AI morning briefing' })).toBeVisible()
  await events.ready('automation.deleted')
  events.emit('automation.deleted', { id: 'daily' })
  await expect(page).toHaveURL(/\/automations$/)
})

test('failed list retries and mutation errors restore optimistic status', async ({ page }) => {
  await mockAutomations(page)
  let fail = true
  await page.route('**/rpc', async (route) => {
    const req = route.request().postDataJSON()
    if ((req.method === 'automations.list' && fail) || req.method === 'automations.pause')
      return route.fulfill({
        json: {
          jsonrpc: '2.0',
          id: req.id,
          error: { code: -32000, message: 'Temporarily unavailable' },
        },
      })
    return route.fallback()
  })
  await page.goto('/automations')
  await expect(page.getByRole('button', { name: 'Reintentar', exact: true })).toBeVisible({
    timeout: 20000,
  })
  fail = false
  await page.getByRole('button', { name: 'Reintentar', exact: true }).click()
  const daily = page
    .locator('.auto-card')
    .filter({ has: page.getByRole('heading', { name: 'AI morning briefing' }) })
  await daily.getByRole('button', { name: 'Pausar', exact: true }).click()
  await expect(daily.getByRole('alert')).toContainText('Temporarily unavailable')
  await expect(daily.getByRole('button', { name: 'Pausar', exact: true })).toBeEnabled()
})
test('attached context can be removed before sending', async ({ page }) => {
  await mockAutomations(page)
  await page.goto('/automations')
  await page
    .locator('.auto-card')
    .first()
    .getByRole('button', { name: 'Editar', exact: true })
    .click()
  await page.getByRole('button', { name: 'Quitar automatización adjunta', exact: true }).click()
  await expect(page.locator('.auto-context-chip')).toHaveCount(0)
  await page.locator('.composer textarea').fill('Hello')
  const sent = page.waitForRequest(
    (r) => r.url().endsWith('/rpc') && r.postDataJSON().method === 'conversation.send',
  )
  await page.getByRole('button', { name: 'Enviar mensaje', exact: true }).click()
  expect((await sent).postDataJSON().params.text).toBe('Hello')
})
