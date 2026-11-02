import { test, expect } from './fixtures'
import { mockWork } from './work-fixtures'

test('create, edit, snooze, complete and delete reminders', async ({ page }) => {
  const state = await mockWork(page)
  await page.goto('/reminders')
  await expect(page.getByText('Review release notes', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Nuevo recordatorio', exact: true }).click()
  let dialog = page.getByRole('dialog')
  await dialog.getByLabel('¿Qué quieres recordar?').fill('Call the team')
  await dialog.getByLabel('Fecha y hora').fill('2026-11-03T10:00')
  await dialog.getByLabel('Zona horaria').fill('America/New_York')
  await dialog.getByRole('combobox', { name: 'Repetición', exact: true }).click()
  await dialog.getByRole('option', { name: 'Cada día', exact: true }).click()
  await dialog.getByRole('button', { name: 'Guardar', exact: true }).click()
  await expect(dialog).toHaveCount(0)
  expect(state.reminders.find((r) => r.text === 'Call the team')?.due_at).toBe(
    '2026-11-03T15:00:00.000Z',
  )
  const row = page.locator('.work-reminder').filter({ hasText: 'Call the team' })
  await row.getByRole('button', { name: 'Editar', exact: true }).click()
  dialog = page.getByRole('dialog')
  await dialog.getByLabel('¿Qué quieres recordar?').fill('Call the design team')
  await dialog.getByRole('button', { name: 'Guardar', exact: true }).click()
  await expect(dialog).toHaveCount(0)
  const edited = page.locator('.work-reminder').filter({ hasText: 'Call the design team' })
  await edited.getByRole('button', { name: 'Posponer', exact: true }).click()
  await page.getByRole('dialog').getByRole('button', { name: '15 minutos', exact: true }).click()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  expect(
    state.calls.find((c) => c.method === 'reminders.update' && c.params.status === 'snoozed')
      ?.params.snooze_until,
  ).toBeTruthy()
  await edited.getByRole('button', { name: 'Completar', exact: true }).click()
  await page.getByRole('tab', { name: 'Completados', exact: true }).click()
  await expect(edited).toBeVisible()
  await edited.getByRole('button', { name: 'Eliminar', exact: true }).click()
  await expect(edited).toHaveCount(0)
})

test('rollback on mutation failure, live fired toast and cancellation', async ({ page }) => {
  const state = await mockWork(page)
  await page.goto('/reminders')
  const row = page.locator('.work-reminder').filter({ hasText: 'Review release notes' })
  await expect(row).toBeVisible()
  state.failMutation = true
  await row.getByRole('button', { name: 'Eliminar', exact: true }).click()
  await expect(page.getByRole('alert')).toContainText('No se guardó el cambio')
  await expect(row).toBeVisible()
  state.failMutation = false
  state.emit('reminder.fired', { reminder: state.reminders[0] })
  await expect(page.getByRole('status')).toContainText('Es hora: Review release notes')
  await expect(row).toHaveClass(/work-highlight/)
  state.reminders[0]!.text = 'Updated reminder'
  state.emit('reminder.updated', { reminder: state.reminders[0] })
  await expect(page.getByText('Updated reminder', { exact: true })).toBeVisible()
  await page
    .locator('.work-reminder')
    .getByRole('button', { name: 'Cancelada', exact: true })
    .click()
  await page.getByRole('tab', { name: 'Cancelados', exact: true }).click()
  await expect(page.getByText('Updated reminder', { exact: true })).toBeVisible()
})

test('reminders transport failure shows retry and no fake records', async ({ page }) => {
  await mockWork(page)
  await page.route('**/rpc', async (route) => {
    if (route.request().postDataJSON().method === 'reminders.list') return route.abort()
    return route.fallback()
  })
  await page.goto('/reminders')
  await expect(page.getByRole('alert')).toContainText('No se pudo conectar')
  await expect(page.locator('.work-reminder')).toHaveCount(0)
})

test('repeat listbox preserves custom cron validation and dialog focus', async ({ page }) => {
  const state = await mockWork(page)
  await page.goto('/reminders')
  await page.getByRole('button', { name: 'Nuevo recordatorio', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('¿Qué quieres recordar?').fill('Review schedule')
  await dialog.getByLabel('Fecha y hora').fill('2026-11-03T10:00')
  await dialog.getByLabel('Zona horaria').fill('UTC')
  const repeat = dialog.getByRole('combobox', { name: 'Repetición', exact: true })
  await repeat.click()
  await page.keyboard.press('Escape')
  await expect(dialog).toBeVisible()
  await expect(repeat).toBeFocused()
  await repeat.click()
  await dialog.getByRole('option', { name: 'Cron personalizado', exact: true }).click()
  await dialog.getByRole('textbox', { name: 'Cron personalizado', exact: true }).fill('invalid')
  await dialog.getByRole('button', { name: 'Guardar', exact: true }).click()
  await expect(dialog.getByRole('alert')).toBeVisible()
  expect(state.calls.some((c) => c.method === 'reminders.create')).toBe(false)
  await dialog.getByRole('textbox', { name: 'Cron personalizado', exact: true }).fill('0 9 * * 1')
  await dialog.getByRole('button', { name: 'Guardar', exact: true }).click()
  await expect(dialog).toHaveCount(0)
  expect(state.reminders.find((r) => r.text === 'Review schedule')?.repeat).toBe('0 9 * * 1')
})
