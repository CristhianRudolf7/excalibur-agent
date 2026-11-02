import { mockWork } from './work-fixtures'
import { mockChat } from './chat-fixture'
import { test, expect } from './fixtures'

test.beforeEach(async ({ page }) => {
  await page.setViewportSize({ width: 1536, height: 1024 })
})
test('desktop sidebar, shortcuts and persisted resize', async ({ page }) => {
  await page.goto('/')
  await page.getByRole('button', { name: 'Alternar panel', exact: true }).click()
  const sidebar = page.locator('.sidebar')
  await expect(sidebar).toBeVisible()
  await page.getByRole('button', { name: 'Alternar barra lateral' }).click()
  await expect(sidebar).toHaveClass(/sidebar-hidden/)
  await expect(page.locator('.sidebar-rail')).toBeVisible()
  await expect(page.locator('.sidebar-slot')).toHaveCSS('width', '64px')
  await page.keyboard.press('Control+b')
  await expect(sidebar).not.toHaveClass(/sidebar-hidden/)
  await expect(sidebar).toHaveCSS('width', '240px')
  await expect(page.locator('.sidebar-slot')).toHaveCSS('width', '241px')
  const handle = page.getByRole('separator', { name: 'Cambiar ancho del panel', exact: true })
  // Wait for the final observer measurement after the sidebar animation.
  await expect
    .poll(() =>
      page.evaluate(() => {
        const split = document.querySelector('.split-layout')!
        const measured = Number(
          document.querySelector('.split-layout [role=separator]')!.getAttribute('aria-valuenow'),
        )
        return measured - Math.round((split.getBoundingClientRect().width - 1) / 2)
      }),
    )
    .toBe(0)
  const initial = Number(await handle.getAttribute('aria-valuenow'))
  const box = (await handle.boundingBox())!
  await page.mouse.move(box.x, box.y + 200)
  await page.mouse.down()
  await page.mouse.move(box.x - 100, box.y + 200, { steps: 10 })
  await page.mouse.up()
  await expect
    .poll(async () => Number(await handle.getAttribute('aria-valuenow')))
    .toBeGreaterThan(initial + 70)
  const width = await handle.getAttribute('aria-valuenow')
  await page.reload()
  await page.getByRole('button', { name: 'Alternar panel', exact: true }).click()
  await expect(handle).toHaveAttribute('aria-valuenow', width!)
  await handle.focus()
  await page.keyboard.press('ArrowRight')
  await expect
    .poll(async () => Number(await handle.getAttribute('aria-valuenow')))
    .toBeLessThan(Number(width))
  await handle.dblclick()
  await expect
    .poll(async () => Number(await handle.getAttribute('aria-valuenow')))
    .toBeCloseTo(initial, 0)
  await page.getByRole('button', { name: 'Alternar panel', exact: true }).click()
  await expect(handle).toHaveCount(0)
  await page.keyboard.press('Control+.')
  await expect(handle).toBeVisible()
})
test('mobile drawer and fullscreen panel', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/')
  const sidebar = page.locator('.sidebar')
  await expect(sidebar).toHaveClass(/sidebar-hidden/)
  await page.getByRole('button', { name: 'Alternar barra lateral' }).last().click()
  await expect(sidebar).not.toHaveClass(/sidebar-hidden/)
  await page.keyboard.press('Escape')
  await expect(sidebar).toHaveClass(/sidebar-hidden/)
  await page.getByRole('button', { name: 'Alternar barra lateral' }).last().click()
  await page.locator('.drawer-backdrop').click({ position: { x: 350, y: 500 } })
  await expect(sidebar).toHaveClass(/sidebar-hidden/)
  await page.getByRole('button', { name: 'Alternar barra lateral' }).last().click()
  await page.locator('.agent-nav').getByRole('button', { name: 'Excalibur' }).click()
  await expect(sidebar).toHaveClass(/sidebar-hidden/)
  await expect(page).toHaveURL(/agents\/excalibur/)
  await page.getByRole('button', { name: 'Alternar panel', exact: true }).click()
  await expect(page.locator('.mobile-panel')).toBeVisible()
  await page.getByRole('button', { name: 'Cerrar', exact: true }).click()
  await expect(page.locator('.mobile-panel')).toHaveCount(0)
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
})
test('long composer, keyboard sending, live reply and plan approval', async ({ page }) => {
  const chat = await mockChat(page)
  await page.route('**/rpc', async (route) => {
    const request = route.request().postDataJSON()
    if (request.method !== 'plan.approve') return route.fallback()
    await route.fulfill({ json: { jsonrpc: '2.0', id: request.id, result: {} } })
  })
  await page.goto('/')
  const input = page.getByRole('textbox', { name: 'Escribe a Excalibur…' })
  await input.fill(
    Array.from({ length: 60 }, (_, i) => `Line ${i} ${'https://example.com/'.repeat(15)}`).join(
      '\n',
    ),
  )
  expect(await input.evaluate((node) => node.scrollHeight > node.clientHeight)).toBe(true)
  expect((await input.boundingBox())!.height).toBeLessThanOrEqual(240)
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await input.fill('First line')
  await input.press('Shift+Enter')
  await input.press('x')
  await expect(input).toHaveValue('First line\nx')
  await input.press('Enter')
  await expect(input).toHaveValue('')
  await expect(input).toBeFocused()
  chat.emit('message.created', {
    message_id: 'reply',
    conversation_id: 'cnv_web_excalibur',
    role: 'assistant',
    text: 'Message received by the daemon.',
  })
  await expect(page.getByText('Message received by the daemon.')).toBeVisible()
  chat.emit('plan.approval_requested', { plan_id: 'p1', summary: 'Verify the work' })
  await page.getByRole('button', { name: 'Aprobar', exact: true }).click()
  await expect(page.getByText('Respuesta enviada', { exact: true })).toBeVisible()
})
test('files panel opens a local Monaco editor, tabs and cursor', async ({ page }) => {
  const errors: string[] = []
  page.on('pageerror', (error) => errors.push(error.message))
  await page.goto('/agents/excalibur?panel=files')
  await page.getByRole('button', { name: /Alternar panel|Toggle panel/ }).click()
  await expect(page).toHaveURL(/panel=files/)
  await expect(page.getByRole('tab', { name: 'auth.ts', exact: true })).toHaveAttribute(
    'aria-selected',
    'true',
  )
  await expect(page.locator('.monaco-editor')).toBeVisible()
  await expect(page.locator('.diff-added').first()).toBeVisible()
  await page.getByRole('tab', { name: 'auth.test.ts', exact: true }).click()
  await expect(page.getByRole('tab', { name: 'auth.test.ts', exact: true })).toHaveAttribute(
    'aria-selected',
    'true',
  )
  await expect(page.locator('.monaco-editor')).toBeVisible()
  expect(errors).toEqual([])
})
test('spreadsheet recalculation, formula, undo, clipboard and independent sheets', async ({
  page,
}) => {
  await page.goto('/agents/atlas?panel=sheet')
  await page.getByRole('button', { name: /Alternar panel|Toggle panel/ }).click()
  const cell = (address: string) =>
    page.getByRole('gridcell', { name: `Celda ${address}`, exact: true })
  await cell('C2').dblclick()
  await cell('C2').getByRole('textbox').fill('121')
  await cell('C2').getByRole('textbox').press('Enter')
  await expect(cell('F2')).toHaveText('2.299,00 €')
  await page.locator('.sheet-viewport').evaluate((node) => {
    node.scrollLeft = 600
  })
  await expect(cell('H2')).toHaveText('1.615,00 €')
  await expect(cell('H8')).toHaveText('7.941,25 €')
  await cell('H4').click()
  await page.getByRole('textbox', { name: 'Fórmula', exact: true }).fill('=SUM(F2:F6)-SUM(G2:G6)')
  await page.getByRole('textbox', { name: 'Fórmula', exact: true }).press('Enter')
  // Formula submission returns focus on the next animation frame before grid shortcuts apply.
  await expect(page.getByRole('grid', { name: 'Hoja de cálculo' })).toBeFocused()
  await expect(cell('H4')).toHaveText('7.941,25 €')
  await page.keyboard.press('Control+z')
  await expect(cell('H4')).toHaveText('1.856,25 €')
  await page.keyboard.press('Control+Shift+z')
  await expect(cell('H4')).toHaveText('7.941,25 €')
  await page.getByRole('tab', { name: 'Previsión', exact: true }).click()
  await page.locator('.sheet-viewport').evaluate((node) => {
    node.scrollLeft = 0
  })
  await cell('A2').click()
  await page.keyboard.type('=ROUND(10/3,2)')
  await page.keyboard.press('Enter')
  await expect(cell('A2')).toHaveText('3,33')
  await page.getByRole('tab', { name: 'Resumen', exact: true }).click()
  await expect(cell('C2')).toHaveText('121')
  await page.getByRole('button', { name: 'Añadir hoja' }).click()
  await expect(page.getByRole('tab', { name: 'Hoja 4' })).toHaveAttribute('aria-selected', 'true')
})
test('projects search and translated interface', async ({ page }) => {
  await mockWork(page)
  await page.goto('/projects')
  await expect(page.getByRole('heading', { name: 'Proyectos', exact: true })).toBeVisible()
  await expect(page.locator('.work-project-card')).toHaveCount(1)
  await page.getByRole('textbox', { name: 'Buscar proyectos' }).fill('Compiler')
  await expect(page.locator('.work-project-card')).toHaveCount(1)
  await page.getByRole('textbox', { name: 'Buscar proyectos' }).fill('missing')
  await expect(page.getByText('Nada por aquí todavía')).toBeVisible()
  await page.getByRole('button', { name: 'Configuración', exact: true }).click()
  await page
    .getByRole('group', { name: 'Idioma de la interfaz' })
    .getByRole('button', { name: 'English', exact: true })
    .click()
  await expect(page.getByRole('heading', { name: 'Settings', exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Projects', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Projects', exact: true })).toBeVisible()
})
test('all pages fit mobile and desktop without console errors', async ({ page }) => {
  await mockWork(page)
  const errors: string[] = []
  page.on('pageerror', (e) => errors.push(e.message))
  page.on('console', (message) => {
    if (message.type() === 'error') errors.push(message.text())
  })
  for (const width of [390, 1024, 1920]) {
    await page.setViewportSize({ width, height: 844 })
    for (const path of [
      '/agents/excalibur?panel=files',
      '/agents/atlas?panel=sheet',
      '/projects',
    ]) {
      await page.goto(path)
      await expect(page.locator('.main-panel')).toBeVisible()
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
        true,
      )
    }
  }
  expect(errors).toEqual([])
})

test('sheet range selection, TSV clipboard, clearing, cancel and column resizing', async ({
  page,
  context,
}) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write'])
  await page.goto('/agents/atlas?panel=sheet')
  await page.getByRole('button', { name: /Alternar panel|Toggle panel/ }).click()
  const cell = (address: string) =>
    page.getByRole('gridcell', { name: `Celda ${address}`, exact: true })
  await cell('C2').click()
  await page.keyboard.press('F2')
  await cell('C2').getByRole('textbox').fill('999')
  await page.keyboard.press('Escape')
  await expect(cell('C2')).toHaveText('120')
  await cell('C2').click()
  await page.keyboard.press('Control+c')
  await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toBe('120')
  await page.evaluate(() => navigator.clipboard.writeText('10\t20\n30\t40'))
  await cell('C3').click()
  await page.keyboard.press('Control+v')
  await expect(cell('C3')).toHaveText('10')
  await expect(cell('D3')).toHaveText('20,00 €')
  await expect(cell('C4')).toHaveText('30')
  await cell('D4').click({ modifiers: ['Shift'] })
  await expect(cell('C3')).toHaveAttribute('aria-selected', 'true')
  await expect(cell('D4')).toHaveAttribute('aria-selected', 'true')
  await page.keyboard.press('Delete')
  await expect(cell('C3')).toHaveText('')
  await expect(cell('D4')).toHaveText('')
  await page.keyboard.press('Control+z')
  await expect(cell('D4')).toHaveText('40,00 €')
  const resizer = page.locator('.column-resizer').first()
  const box = (await resizer.boundingBox())!
  const oldWidth = (await cell('A2').boundingBox())!.width
  await page.mouse.move(box.x + 3, box.y + 12)
  await page.mouse.down()
  await page.mouse.move(box.x + 55, box.y + 12)
  await page.mouse.up()
  await expect
    .poll(async () => (await cell('A2').boundingBox())!.width)
    .toBeGreaterThan(oldWidth + 40)
})

test('real tasks, keyboard launchers and activity view', async ({ page }) => {
  await mockWork(page)
  await page.goto('/tasks')
  await expect(page.getByRole('button', { name: /EXC-201/ })).toBeVisible()
  await page.goto('/')
  await expect(page.getByRole('tab', { name: 'Tareas', exact: true })).toHaveCount(0)
  await expect(page.getByRole('tab', { name: 'Biblioteca', exact: true })).toHaveCount(0)
  await page.getByRole('button', { name: 'Alternar panel', exact: true }).click()
  await page.locator('.panel-launcher').filter({ hasText: 'Pizarra' }).focus()
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL(/panel=whiteboard/)
  await page.getByRole('button', { name: 'Volver al inicio del panel' }).click()
  await page.locator('.panel-launcher').filter({ hasText: 'Actividad' }).click()
  await expect(page).toHaveURL(/panel=activity/)
  await expect(page.getByText('Excalibur no está ejecutando subagentes ahora mismo.')).toBeVisible()
  // The agent info action was removed from the chat header by request.
  await expect(page.getByRole('button', { name: 'Información del agente' })).toHaveCount(0)
})

test('mobile sheet keeps headers and close control visible', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/agents/atlas?panel=sheet')
  await page.getByRole('button', { name: /Alternar panel|Toggle panel/ }).click()
  const close = page.getByRole('button', { name: 'Cerrar', exact: true })
  await expect(close).toBeInViewport()
  await expect(page.getByRole('gridcell', { name: 'Celda H4', exact: true })).toBeInViewport()
  await page.getByRole('textbox', { name: 'Celda activa' }).fill('A1')
  await expect(page.getByRole('gridcell', { name: 'Celda A1', exact: true })).toHaveText('Concepto')
  const header = (await page.getByRole('columnheader', { name: 'A', exact: true }).boundingBox())!
  const firstRow = (await page
    .getByRole('gridcell', { name: 'Celda A1', exact: true })
    .boundingBox())!
  expect(header.y + header.height).toBeLessThanOrEqual(firstRow.y)
  await page.getByRole('gridcell', { name: 'Celda C2', exact: true }).dblclick()
  const input = page.getByRole('gridcell', { name: 'Celda C2', exact: true }).getByRole('textbox')
  await input.fill('123')
  await input.press('Enter')
  await expect(page.getByRole('gridcell', { name: 'Celda C2', exact: true })).toHaveText('123')
  await close.click()
  await expect(page.locator('.mobile-panel')).toHaveCount(0)
})

for (const width of [1440, 390]) {
  test(`panel surfaces, launchers and reduced motion at ${width}px`, async ({ page }) => {
    test.setTimeout(90000)
    await page.setViewportSize({ width, height: width === 390 ? 844 : 900 })
    await page.route('**/rpc', async (route) => {
      const request = route.request().postDataJSON()
      if (!/^(library|whiteboard)\./.test(request.method)) return route.fallback()
      await route.fulfill({
        json: { jsonrpc: '2.0', id: request.id, error: { code: -32601, message: 'Unavailable' } },
      })
    })
    await page.goto('/agents/excalibur?panel=document&view=chat')
    await page.getByRole('button', { name: /Alternar panel|Toggle panel/ }).click()
    const surface = await page.evaluate(() => {
      const probe = document.createElement('div')
      probe.style.backgroundColor = 'var(--surface)'
      document.body.append(probe)
      const color = getComputedStyle(probe).backgroundColor
      probe.remove()
      return color
    })
    await expect(page.locator('.viewer-empty')).toHaveCSS('background-color', surface)
    // The retained document viewer remains available through a deep link.
    await expect(page.locator('.panel-navigation')).toContainText('Documento')
    await page.getByRole('button', { name: 'Volver al inicio del panel' }).click()
    for (const [view, name] of [
      ['files', 'Archivos'],
      ['whiteboard', 'Pizarra'],
    ]) {
      await page.locator('.panel-launcher').filter({ hasText: name }).click()
      await expect(page).toHaveURL(new RegExp(`panel=${view}`))
      if (view === 'files') await expect(page.locator('.monaco-editor').first()).toBeVisible()
      await page.screenshot({
        path: test.info().outputPath(`${view}-${width}.png`),
        animations: 'disabled',
      })
      await page.getByRole('button', { name: 'Volver al inicio del panel' }).click()
    }
    await page.goto('/agents/excalibur?panel=document&view=chat')
    await page.getByRole('button', { name: 'Alternar panel', exact: true }).click()
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await expect(page.locator('.viewer-empty')).toHaveCSS('animation-name', 'none')
    await expect(page.locator('.panel-slide')).toHaveCSS('animation-name', 'none')
    await page.evaluate(() => document.documentElement.classList.add('dark'))
    const darkSurface = await page.evaluate(() => {
      const probe = document.createElement('div')
      probe.style.backgroundColor = 'var(--surface)'
      document.body.append(probe)
      const color = getComputedStyle(probe).backgroundColor
      probe.remove()
      return color
    })
    await expect(page.locator('.viewer-empty')).toHaveCSS('background-color', darkSurface)
    await page.getByRole('button', { name: 'Volver al inicio del panel' }).click()
    await page.locator('.panel-launcher').filter({ hasText: 'Archivos' }).click()
    await expect(page.locator('.monaco-editor').first()).toHaveCSS('background-color', darkSurface)
    await page.screenshot({
      path: test.info().outputPath(`files-dark-${width}.png`),
      animations: 'disabled',
    })
    await page.evaluate(() => document.documentElement.classList.remove('dark'))
    await expect(page.locator('.monaco-editor').first()).toHaveCSS('background-color', surface)
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  })
}
