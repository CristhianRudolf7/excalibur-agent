import { test, expect } from './fixtures'
import { mockWork } from './work-fixtures'

test('real projects, search, detail tasks, lazy explorer and registration validation', async ({
  page,
}) => {
  const state = await mockWork(page)
  await page.goto('/projects')
  await page.getByRole('textbox', { name: 'Buscar proyectos' }).fill('Compiler')
  await page.getByRole('button', { name: /Compiler Build tools/ }).click()
  await expect(page.getByRole('button', { name: /EXC-201/ })).toBeVisible()
  expect(
    state.calls.some((c) => c.method === 'tasks.query' && c.params.project_id === 'project-1'),
  ).toBe(true)
  await page.getByRole('tab', { name: 'Archivos', exact: true }).click()
  await expect(page.getByRole('button', { name: 'README.md', exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'src', exact: true }).click()
  await expect(page.getByRole('button', { name: 'main.go', exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Nuevo proyecto', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('Nombre del proyecto').fill('New workspace')
  await dialog.getByLabel('Directorio absoluto').fill('relative/path')
  await dialog.getByRole('button', { name: 'Guardar', exact: true }).click()
  await expect(dialog.getByRole('alert')).toContainText('ruta absoluta')
  expect(state.calls.some((c) => c.method === 'project.register')).toBe(false)
  await dialog.getByLabel('Directorio absoluto').fill('/workspace/new')
  await dialog.getByRole('button', { name: 'Guardar', exact: true }).click()
  await expect(dialog).toHaveCount(0)
  await expect(page.getByRole('heading', { name: 'New workspace', exact: true })).toBeVisible()
  await expect(page.getByRole('tab', { name: 'Mis tareas', exact: true })).toHaveAttribute(
    'aria-selected',
    'true',
  )
})

test('project service unavailable and registration errors are explicit', async ({ page }) => {
  const state = await mockWork(page)
  state.unavailable.add('project.list')
  await page.goto('/projects')
  await expect(page.getByRole('alert')).toContainText('No se pudo conectar')
  await expect(page.locator('.work-project-card')).toHaveCount(0)
  state.unavailable.clear()
  await page.getByRole('button', { name: 'Reintentar', exact: true }).click()
  await expect(page.locator('.work-project-card')).toHaveCount(1)
  state.failMutation = true
  await page.getByRole('button', { name: 'Nuevo proyecto', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('Nombre del proyecto').fill('New workspace')
  await dialog.getByLabel('Directorio absoluto').fill('/workspace/new')
  await dialog.getByRole('button', { name: 'Guardar', exact: true }).click()
  await expect(dialog.getByRole('alert')).toBeVisible()
})
