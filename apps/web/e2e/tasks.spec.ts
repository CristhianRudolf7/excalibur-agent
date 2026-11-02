import { test, expect } from './fixtures'
import { mockWork } from './work-fixtures'

test('task list, cursor, filters, board, details and live status', async ({ page }) => {
  const state = await mockWork(page)
  await page.goto('/tasks')
  await expect(page.getByRole('button', { name: /EXC-201/ })).toBeVisible()
  await page.getByRole('button', { name: 'Cargar más', exact: true }).click()
  await expect(page.getByRole('button', { name: /EXC-202/ })).toBeVisible()
  await page.getByRole('textbox', { name: 'Buscar tareas' }).fill('Ship')
  await expect(page.getByRole('button', { name: /EXC-202/ })).toHaveCount(0)
  await page.getByRole('combobox', { name: 'Proyecto', exact: true }).click()
  await page.getByRole('option', { name: 'Compiler', exact: true }).click()
  await expect
    .poll(() =>
      state.calls.some(
        (c) =>
          c.method === 'tasks.query' &&
          c.params.project_id === 'project-1' &&
          c.params.text === 'Ship',
      ),
    )
    .toBe(true)
  await page.getByRole('button', { name: 'Tablero', exact: true }).click()
  await expect(page.locator('.work-board')).toBeVisible()
  await page.getByRole('button', { name: /EXC-201/ }).click()
  const drawer = page.getByRole('dialog')
  await expect(drawer.getByText('Deliver a working compiler.')).toBeVisible()
  for (const text of [
    'Compile source files',
    'All tests green',
    'Depende de: step-1',
    'Ready for review',
    'run-1',
  ])
    await expect(drawer.getByText(text, { exact: false })).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(drawer).toHaveCount(0)
  state.tasks[0]!.status = 'done'
  state.emit('task.updated', { task: state.tasks[0] })
  await expect(page.locator('.work-task .status-done')).toBeVisible()
  await page.getByRole('tab', { name: /Completada/ }).click()
  await expect(page.getByRole('tab', { name: /Completada/ })).toHaveAttribute(
    'aria-selected',
    'true',
  )
})

test('chat panel has no duplicate tasks tab and mobile navigation reaches reminders', async ({
  page,
}) => {
  const state = await mockWork(page)
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/agents/excalibur?view=chat&panel=tasks')
  await page.getByRole('button', { name: 'Alternar panel', exact: true }).click()
  await expect(
    page.locator('.mobile-panel').getByRole('tab', { name: 'Tareas', exact: true }),
  ).toHaveCount(0)
  await expect(
    page.locator('.mobile-panel').getByRole('button', { name: /Archivos/ }),
  ).toBeVisible()
  expect(state.calls.some((c) => c.method === 'tasks.query')).toBe(false)
  await page.locator('.mobile-panel').getByRole('button', { name: 'Cerrar', exact: true }).click()
  await page.getByRole('button', { name: 'Alternar barra lateral' }).first().click()
  await page.locator('.sidebar').getByRole('button', { name: 'Recordatorios', exact: true }).click()
  await expect(page).toHaveURL(/\/reminders/)
  await expect(page.getByRole('heading', { name: 'Recordatorios', exact: true })).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
})

test('task method unavailable and empty states', async ({ page }) => {
  const state = await mockWork(page)
  state.unavailable.add('tasks.query')
  await page.goto('/tasks')
  await expect(page.getByRole('alert')).toContainText('No se pudo conectar')
  state.unavailable.clear()
  state.tasks.length = 0
  await page.getByRole('button', { name: 'Reintentar', exact: true }).click()
  await expect(page.getByText('Nada por aquí todavía')).toBeVisible()
})

test('work views fit 390px with reduced motion and retain keyboard controls', async ({
  page,
}, testInfo) => {
  await mockWork(page)
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.setViewportSize({ width: 390, height: 844 })
  for (const route of ['tasks', 'reminders', 'projects']) {
    await page.goto(`/${route}`)
    await expect(page.locator('.work-header h1')).toBeVisible()
    await expect(page.locator('.work-skeleton')).toHaveCount(0)
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    await page.screenshot({
      animations: 'disabled',
      path: testInfo.outputPath(`${route}-mobile.png`),
    })
  }
  await page.goto('/tasks')
  const all = page.getByRole('tab', { name: /^Todos/ })
  await all.focus()
  await page.keyboard.press('ArrowRight')
  await expect(page.getByRole('tab', { name: /Pendiente/ })).toHaveAttribute(
    'aria-selected',
    'true',
  )
  await page.keyboard.press('End')
  await expect(page.getByRole('tab', { name: /Cancelada/ })).toHaveAttribute(
    'aria-selected',
    'true',
  )
})

test('work redesign uses daemon counts, accessible progress and reduced motion', async ({
  page,
}) => {
  const state = await mockWork(page)
  state.tasks[0]!.due_at = '2026-11-03T10:00:00Z'
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.goto('/tasks')
  await expect(page.locator('.work-metric').nth(1).locator('strong')).toHaveText('1')
  await expect(page.locator('.work-metric-ink strong')).toHaveText('0')
  await expect(page.getByRole('img', { name: '1 de 2 criterios cumplidos' })).toBeVisible()
  await expect(page.locator('.work-task-due')).toHaveAttribute('datetime', '2026-11-03T10:00:00Z')
  await expect(page.locator('.work-task .work-status-in_progress')).toBeVisible()
  await expect(page.locator('.work-tab-indicator')).toHaveCSS('transition-duration', '0s')
  await expect(page.locator('.work-task')).toHaveCSS('animation-name', 'none')
  state.tasks[0]!.status = 'done'
  state.emit('task.updated', { task: state.tasks[0] })
  await expect(page.locator('.work-metric-ink strong')).toHaveText('1')
  await expect(page.locator('.work-task .work-status-done')).toBeVisible()
})

test('work screens at desktop and mobile sizes', async ({ page }, testInfo) => {
  test.setTimeout(180_000)
  const capture = async (name: string) => {
    await page.evaluate(async () => {
      await Promise.all(
        document
          .getAnimations()
          .filter((animation) => animation.effect?.getTiming().iterations !== Infinity)
          .map((animation) => animation.finished.catch(() => {})),
      )
    })
    await page.screenshot({ path: testInfo.outputPath(name) })
  }
  const state = await mockWork(page)
  state.tasks[0]!.due_at = '2026-11-03T10:00:00Z'
  await page.emulateMedia({ colorScheme: 'light' })
  for (const viewport of [
    { width: 1440, height: 900 },
    { width: 390, height: 844 },
  ]) {
    await page.setViewportSize(viewport)
    for (const route of ['tasks', 'reminders', 'projects']) {
      await page.goto(`/${route}`)
      await expect(page.locator('.work-header h1')).toBeVisible()
      await expect(page.locator('.work-skeleton')).toHaveCount(0)
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
        true,
      )
      await capture(`${route}-${viewport.width}.png`)
    }
    await page.goto('/tasks')
    await page.getByRole('combobox', { name: 'Proyecto', exact: true }).click()
    await expect(page.getByRole('option', { name: 'Compiler', exact: true })).toBeVisible()
    await capture(`project-filter-${viewport.width}.png`)
    await page.keyboard.press('Escape')
    await page.getByRole('button', { name: /EXC-201/ }).click()
    await expect(page.getByRole('dialog').getByText('Deliver a working compiler.')).toBeVisible()
    await capture(`task-detail-${viewport.width}.png`)
    await page.keyboard.press('Escape')
    await page.goto('/reminders')
    await expect(page.locator('.work-next-due')).toBeVisible()
    await page.getByRole('button', { name: 'Nuevo recordatorio', exact: true }).click()
    await expect(page.getByRole('dialog')).toBeVisible()
    await capture(`reminder-dialog-${viewport.width}.png`)
    await page.getByRole('combobox', { name: 'Repetición', exact: true }).click()
    await expect(page.getByRole('option', { name: 'Cada día', exact: true })).toBeVisible()
    await expect(
      page.getByRole('option', { name: 'Cron personalizado', exact: true }),
    ).toBeInViewport({ ratio: 1 })
    await expect
      .poll(async () => {
        const menu = (await page.locator('.work-select-menu').boundingBox())!
        const dialog = (await page.getByRole('dialog').boundingBox())!
        return menu.y >= dialog.y && menu.y + menu.height <= dialog.y + dialog.height
      })
      .toBe(true)
    await capture(`reminder-repeat-${viewport.width}.png`)
    await page.keyboard.press('Escape')
    await page.keyboard.press('Escape')
    await page.goto('/tasks')
    await page.getByRole('button', { name: 'Cargar más', exact: true }).click()
    await page.getByRole('button', { name: 'Tablero', exact: true }).click()
    await expect(page.locator('.work-board')).toBeVisible()
    await capture(`task-board-${viewport.width}.png`)
  }
  await page.evaluate(() => localStorage.setItem('excalibur-ui-theme', 'dark'))
  await page.goto('/tasks')
  await expect(page.locator('html')).toHaveClass(/dark/)
  await expect(page.locator('.work-task')).toBeVisible()
  await expect(page.locator('.work-metric').nth(1).locator('strong')).toHaveText('1')
  await capture('tasks-dark.png')
})

test('project listbox supports keyboard selection, dismissal and filter reset', async ({
  page,
}) => {
  const state = await mockWork(page)
  await page.goto('/tasks')
  await expect(page.getByText('Tareas con criterios y evidencias', { exact: true })).toBeVisible()
  await expect(page.locator('.work-metric-caption')).toHaveText([
    'tareas',
    'tareas',
    'tareas',
    'tareas',
  ])
  const filter = page.getByRole('combobox', { name: 'Proyecto', exact: true })
  await filter.focus()
  await filter.press('Enter')
  await expect(page.getByRole('option', { name: 'Todos', exact: true })).toBeFocused()
  await page.keyboard.press('End')
  await expect(page.getByRole('option', { name: 'Compiler', exact: true })).toBeFocused()
  await page.keyboard.press('Enter')
  await expect(filter).toHaveText('Compiler')
  await expect
    .poll(() =>
      state.calls.some((c) => c.method === 'tasks.query' && c.params.project_id === 'project-1'),
    )
    .toBe(true)
  await filter.click()
  await page.keyboard.press('Escape')
  await expect(page.getByRole('listbox')).toHaveCount(0)
  await expect(filter).toBeFocused()
  await filter.click()
  await page.getByRole('option', { name: 'Todos', exact: true }).click()
  await expect(filter).toHaveText('Todos')
  await expect(page.getByRole('button', { name: 'Cargar más', exact: true })).toBeVisible()
})

for (const width of [1440, 390]) {
  test(`task status tabs keep full labels and reveal keyboard selection at ${width}px`, async ({
    page,
  }) => {
    await mockWork(page)
    await page.setViewportSize({ width, height: width === 390 ? 844 : 900 })
    await page.goto('/tasks')
    const tabs = page.locator('.work-page .work-tabs')
    const all = tabs.getByRole('tab', { name: /^Todos/ })
    await all.focus()
    await page.keyboard.press('End')
    const cancelled = tabs.getByRole('tab', { name: /Cancelada/ })
    await expect(cancelled).toHaveAttribute('aria-selected', 'true')
    await expect(cancelled).toBeInViewport()
    expect(await cancelled.evaluate((node) => node.scrollWidth <= node.clientWidth)).toBe(true)
    await expect
      .poll(() =>
        tabs.evaluate((node) => {
          const selected = node.querySelector('[aria-selected="true"]')!.getBoundingClientRect()
          const indicator = node.querySelector('.work-tab-indicator')!.getBoundingClientRect()
          return (
            Math.abs(selected.left - indicator.left) + Math.abs(selected.width - indicator.width)
          )
        }),
      )
      .toBeLessThan(2)
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  })
}
