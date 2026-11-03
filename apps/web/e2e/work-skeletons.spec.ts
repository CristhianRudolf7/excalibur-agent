import { test, expect, type Page } from './fixtures'
import { mockWork } from './work-fixtures'
import { seedDocument, seedPages, seedChunk, seedPDF } from '../src/features/viewer/seeds'

const shots = 'test-results/sk2-shots'
async function review(page: Page, shape: string, width: number, label: RegExp) {
  const status = page
    .getByRole('status', { name: label })
    .filter({ has: page.locator('.skeleton') })
    .first()
  await expect(status).toBeVisible()
  await expect(status).toHaveAttribute('aria-busy', 'true')
  await expect(status.locator('.skeleton:visible').first()).toBeVisible()
  await expect(status).toHaveText('')
  await expect(
    page.getByText(
      /^(Cargando(?: editor| biblioteca| documento| pizarra)?…|Loading(?: editor| library| document| whiteboard)?…)$/,
    ),
  ).toHaveCount(0)
  await page.screenshot({ path: `${shots}/sk2-${shape}-${width}.png` })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  expect(
    await status
      .locator('.skeleton')
      .first()
      .evaluate((el) => getComputedStyle(el, '::after').animationName),
  ).toBe('none')
  await page.emulateMedia({ reducedMotion: 'no-preference' })
}
for (const width of [1440, 390]) {
  for (const view of ['tasks', 'board', 'reminders', 'projects']) {
    test(`${view} skeleton at ${width}`, async ({ page }) => {
      await page.setViewportSize({ width, height: width === 390 ? 844 : 900 })
      await mockWork(page)
      let release!: () => void
      const gate = new Promise<void>((resolve) => {
        release = resolve
      })
      await page.route('**/rpc', async (route) => {
        const { method } = route.request().postDataJSON()
        if (
          method ===
          {
            tasks: 'tasks.query',
            board: 'tasks.query',
            reminders: 'reminders.list',
            projects: 'project.list',
          }[view]
        )
          await gate
        await route.fallback()
      })
      await page.goto(`/${view === 'board' ? 'tasks' : view}`)
      if (view === 'board') await page.getByRole('button', { name: 'Tablero', exact: true }).click()
      await expect(page.locator(`.work-loading-${view}`)).toBeVisible()
      await review(page, view, width, /Cargando|Loading/)
      if (view === 'tasks' && width === 1440) {
        await expect(page.locator('.work-loading-metrics')).toBeVisible()
        await page.evaluate(() => document.documentElement.classList.add('dark'))
        await page.screenshot({ path: `${shots}/sk2-tasks-dark.png` })
      }
      release()
      await expect(page.locator('.work-loading')).toHaveCount(0)
      await expect(
        page
          .locator(
            view === 'projects'
              ? '.work-project-card'
              : view === 'reminders'
                ? '.work-reminder'
                : '.work-task',
          )
          .first(),
      ).toBeVisible()
    })
  }
  for (const shape of ['paper', 'slides', 'boards', 'canvas', 'editor', 'grid']) {
    test(`${shape} panel skeleton at ${width}`, async ({ page }) => {
      await page.setViewportSize({ width, height: width === 390 ? 844 : 900 })
      let release!: () => void
      const gate = new Promise<void>((resolve) => {
        release = resolve
      })
      const document = seedDocument(
        shape === 'slides' ? 'demo-pptx' : shape === 'grid' ? 'demo-xlsx' : 'demo-docx',
      )
      const board = {
        id: 'board-1',
        conversation_id: 'cnv_web_excalibur',
        title: 'Design board',
        version: 0,
        elements: [],
        updated_at: '2026-11-02T00:00:00Z',
      }
      await page.route('**/rpc', async (route) => {
        const request = route.request().postDataJSON()
        if (!/^(library|whiteboard)\./.test(request.method)) return route.fallback()
        if (
          request.method === 'library.list' ||
          request.method === 'library.pages' ||
          request.method === 'whiteboard.list' ||
          request.method === 'whiteboard.get'
        )
          await gate
        const result =
          request.method === 'library.get'
            ? { document }
            : request.method === 'library.chunk'
              ? { chunk: seedChunk(document.id, 'demo-citation') }
              : request.method === 'library.pages'
                ? { document, pages: seedPages(document.id) }
                : request.method === 'library.list'
                  ? { documents: [], next_cursor: '' }
                  : request.method === 'whiteboard.list'
                    ? { boards: [board] }
                    : board
        await route.fulfill({ json: { jsonrpc: '2.0', id: request.id, result } })
      })
      if (shape === 'editor') {
        await page.route('**/CodeEditor-*.js', async (route) => {
          await gate
          await route.continue()
        })
      }
      const panel = ['boards', 'canvas'].includes(shape)
        ? 'whiteboard'
        : shape === 'editor'
          ? 'files'
          : shape === 'library'
            ? 'library'
            : 'document'
      await page.goto(`/agents/excalibur?panelDeepLink=1&panelDeepLink=1&panel=${panel}&view=chat`)
      await page.getByRole('button', { name: /Alternar panel|Toggle panel/, exact: true }).click()
      if (['paper', 'slides', 'grid'].includes(shape))
        await page
          .getByRole('button', {
            name: shape === 'slides' ? 'PPTX' : shape === 'grid' ? 'XLSX' : 'DOCX',
            exact: true,
          })
          .click()
      if (shape === 'canvas') {
        release()
        await page.getByRole('button', { name: 'Design board', exact: true }).waitFor()
        const nextGate = new Promise<void>((resolve) => {
          release = resolve
        })
        await page.route('**/rpc', async (route) => {
          if (route.request().postDataJSON().method !== 'whiteboard.get') return route.fallback()
          await nextGate
          await route.fallback()
        })
        await page.getByRole('button', { name: 'Design board', exact: true }).click()
      }
      await expect(page.locator(`.panel-placeholder-${shape}`)).toBeVisible()
      await review(page, shape, width, /Cargando|Loading/)
      release()
      await expect(page.locator('.panel-placeholder')).toHaveCount(0, { timeout: 45000 })
      if (shape === 'editor') await expect(page.locator('.monaco-editor').first()).toBeVisible()
      else if (shape === 'canvas')
        await expect(page.getByTestId('whiteboard-surface')).toBeVisible()
      else if (shape === 'boards')
        await expect(page.getByRole('button', { name: 'Design board', exact: true })).toBeVisible()
      else if (shape === 'library')
        await expect(page.getByText(/No hay documentos\.|No documents found\./)).toBeVisible()
      else await expect(page.locator('.viewer-body')).toBeVisible()
    })
  }
}

test('next-page rows append while keeping the load-more accessible name', async ({ page }) => {
  await mockWork(page)
  let release!: () => void
  const gate = new Promise<void>((resolve) => {
    release = resolve
  })
  await page.route('**/rpc', async (route) => {
    const request = route.request().postDataJSON()
    if (request.method === 'tasks.query' && request.params?.cursor) await gate
    await route.fallback()
  })
  await page.goto('/tasks')
  const more = page.getByRole('button', { name: 'Cargar más', exact: true })
  await more.click()
  await expect(more).toHaveAttribute('aria-busy', 'true')
  await expect(page.locator('.work-task').first()).toBeVisible()
  await expect(page.locator('.work-loading-tasks .work-loading-item')).toHaveCount(3)
  release()
  await expect(page.getByRole('button', { name: /EXC-202/ })).toBeVisible()
  await expect(page.locator('.work-loading')).toHaveCount(0)
})

for (const width of [1440, 390]) {
  test(`task details and project explorer placeholders at ${width}`, async ({ page }) => {
    await page.setViewportSize({ width, height: width === 390 ? 844 : 900 })
    await mockWork(page)
    let release!: () => void
    let gate = new Promise<void>((resolve) => {
      release = resolve
    })
    await page.route('**/rpc', async (route) => {
      if (
        ['tasks.get', 'project.get', 'files.list'].includes(route.request().postDataJSON().method)
      )
        await gate
      await route.fallback()
    })
    await page.goto('/tasks')
    await page.getByRole('button', { name: /EXC-201/ }).click()
    await expect(page.locator('.work-loading-detail')).toBeVisible()
    await review(page, 'task-detail', width, /Cargando|Loading/)
    release()
    await expect(page.getByText('Deliver a working compiler.')).toBeVisible()
    await page.keyboard.press('Escape')
    gate = new Promise<void>((resolve) => {
      release = resolve
    })
    await page.goto('/projects')
    await page.getByRole('button', { name: /Compiler Build tools/ }).click()
    await expect(page.locator('.work-loading-detail')).toBeVisible()
    await review(page, 'project-detail', width, /Cargando|Loading/)
    release()
    await expect(page.getByRole('tab', { name: 'Archivos', exact: true })).toBeVisible()
    gate = new Promise<void>((resolve) => {
      release = resolve
    })
    await page.getByRole('tab', { name: 'Archivos', exact: true }).click()
    await expect(page.locator('.work-loading-files')).toBeVisible()
    await review(page, 'explorer', width, /Cargando|Loading/)
    release()
    await expect(page.getByRole('button', { name: 'README.md', exact: true })).toBeVisible()
  })
}

for (const kind of ['projects', 'reminders']) {
  test(`${kind} retains the save label while pending`, async ({ page }) => {
    await mockWork(page)
    let release!: () => void
    const gate = new Promise<void>((resolve) => {
      release = resolve
    })
    await page.route('**/rpc', async (route) => {
      if (
        route.request().postDataJSON().method ===
        (kind === 'projects' ? 'project.register' : 'reminders.create')
      )
        await gate
      await route.fallback()
    })
    await page.goto(`/${kind}`)
    await page
      .getByRole('button', {
        name: kind === 'projects' ? 'Nuevo proyecto' : 'Nuevo recordatorio',
        exact: true,
      })
      .click()
    const dialog = page.getByRole('dialog')
    if (kind === 'projects') {
      await dialog.getByLabel('Nombre del proyecto').fill('Pending project')
      await dialog.getByLabel('Directorio absoluto').fill('/workspace/pending')
    } else {
      await dialog.getByLabel('¿Qué quieres recordar?').fill('Pending reminder')
      await dialog.getByLabel('Fecha y hora').fill('2026-11-03T10:00')
    }
    const save = dialog.getByRole('button', { name: 'Guardar', exact: true })
    await save.click()
    await expect(save).toHaveAttribute('aria-busy', 'true')
    await expect(save).toBeDisabled()
    await expect(save.locator('.work-pending-spinner')).toBeVisible()
    await expect(save).toHaveText('Guardar')
    release()
    await expect(dialog).toHaveCount(0)
  })
}

for (const width of [1440, 390]) {
  test(`PDF rendering skeleton at ${width}`, async ({ page }) => {
    await page.setViewportSize({ width, height: width === 390 ? 844 : 900 })
    let release!: () => void
    const gate = new Promise<void>((resolve) => {
      release = resolve
    })
    const digest = 'a'.repeat(64)
    const document = { ...seedDocument('demo-pdf'), blob_digest: digest }
    await page.route('**/rpc', async (route) => {
      const request = route.request().postDataJSON()
      if (!request.method.startsWith('library.')) return route.fallback()
      const result =
        request.method === 'library.get'
          ? { document }
          : request.method === 'library.chunk'
            ? { chunk: seedChunk(document.id, 'demo-citation') }
            : { document, pages: seedPages(document.id) }
      await route.fulfill({ json: { jsonrpc: '2.0', id: request.id, result } })
    })
    await page.route(`**/blobs/${digest}`, async (route) => {
      await gate
      await route.fulfill({ body: Buffer.from(seedPDF()), contentType: 'application/pdf' })
    })
    await page.goto('/agents/excalibur?panelDeepLink=1&panelDeepLink=1&panel=document&view=chat')
    await page.getByRole('button', { name: /Alternar panel|Toggle panel/ }).click()

    await page.getByRole('button', { name: 'PDF', exact: true }).click()
    await expect(page.locator('.viewer-pdf .panel-placeholder-paper')).toBeVisible()
    await review(page, 'pdf', width, /Cargando|Loading/)
    release()
    await expect(page.locator('.viewer-pdf .panel-placeholder-paper')).toHaveCount(0)
    await expect(page.locator('.viewer-pdf canvas')).toBeVisible()
    await expect(page.locator('.viewer-pdf-highlight')).toBeVisible()
  })
}
