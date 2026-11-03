import { mockEvents } from './event-fixture'
import { test, expect, type Page } from './fixtures'
import { exampleCollection, exampleRecords } from '../src/features/mydata/fixtures'
import type { RecordRow } from '../src/lib/rpc/contract'

async function mockData(page: Page) {
  let records: RecordRow[] = structuredClone(exampleRecords)
  const requests: { method: string; params: Record<string, unknown> }[] = []
  await page.route('**/blobs/**', (route) =>
    route.fulfill({ body: 'Course,Grade\nMathematics,18', contentType: 'text/csv' }),
  )
  await page.route('**/rpc', async (route) => {
    const { id, method, params = {} } = route.request().postDataJSON()
    if (!method.startsWith('collections.') && !method.startsWith('records.'))
      return route.fallback()
    requests.push({ method, params })
    const collection = { ...exampleCollection, record_count: records.length }
    let result: unknown = {}
    if (method === 'collections.list') result = { collections: [collection] }
    if (method === 'collections.get') result = { collection }
    if (method === 'collections.summary')
      result = {
        cards: [
          {
            id: 'avg',
            title: 'Average grade',
            value: 17,
            series: [
              { label: '2026-I', value: 16 },
              { label: '2026-II', value: 18 },
            ],
          },
        ],
      }
    if (method === 'records.list') {
      const filtered = records
        .filter(
          (row) =>
            !params.query ||
            String(row.values.course).toLowerCase().includes(params.query.toLowerCase()),
        )
        .filter((row) =>
          (params.filters ?? []).every((filter: { field: string; op: string; value: unknown }) =>
            filter.op === 'gte'
              ? Number(row.values[filter.field]) >= Number(filter.value)
              : filter.op === 'contains'
                ? String(row.values[filter.field]).includes(String(filter.value))
                : row.values[filter.field] === filter.value,
          ),
        )
      result = { records: filtered, total: filtered.length }
    }
    if (method === 'records.get')
      result = { record: records.find((row) => row.id === params.id), related: [] }
    if (method === 'records.create') {
      const record = {
        id: 'created',
        values: params.values,
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
      }
      records.push(record)
      result = { record }
    }
    if (method === 'records.update') {
      records = records.map((row) =>
        row.id === params.id ? { ...row, values: params.values } : row,
      )
      result = { record: records.find((row) => row.id === params.id) }
    }
    if (method === 'records.delete') {
      records = records.filter((row) => row.id !== params.id)
      result = { ok: true }
    }
    if (method === 'collections.export')
      result = { blob_digest: 'a'.repeat(64), name: `grades.${params.format}`, mime: 'text/csv' }
    await route.fulfill({ json: { jsonrpc: '2.0', id, result } })
  })
  return requests
}
const screenshotPath = (name: string) =>
  process.env.EXCALIBUR_MYDATA_SCREENSHOT_DIR
    ? `${process.env.EXCALIBUR_MYDATA_SCREENSHOT_DIR}/${name}`
    : test.info().outputPath(name)
test('overview → list → create → view → edit → delete', async ({ page }) => {
  const requests = await mockData(page)
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.goto('/data')
  await expect(page.getByRole('heading', { name: 'Mis datos', exact: true })).toBeVisible()
  await page.screenshot({ path: screenshotPath('after-overview.png'), fullPage: true })
  await page.getByRole('link', { name: /University grades/ }).click()
  await expect(page.getByRole('heading', { name: 'Average grade' })).toBeVisible()
  await page.screenshot({ path: screenshotPath('after-list.png'), fullPage: true })
  await page.getByRole('link', { name: 'Crear registro' }).click()
  await page.getByLabel('Course', { exact: false }).first().fill('Algorithms')
  await page.getByLabel('Grade', { exact: true }).fill('19,5')
  await page.getByLabel('Credits', { exact: true }).fill('4')
  await page.getByLabel('Tuition').fill('500,50')
  await page.getByLabel('Exam date').fill('2026-12-15')
  await page.getByLabel('Passed', { exact: true }).check()
  await page.getByLabel('Semester').selectOption('2026-II')
  await page.getByLabel('Core', { exact: true }).check()
  await page.getByLabel('Notes', { exact: true }).fill('Final exam')
  await page.getByLabel('Attachment', { exact: true }).setInputFiles({
    name: 'notes.txt',
    mimeType: 'text/plain',
    buffer: Buffer.from('Study notes'),
  })
  await expect(page.getByRole('link', { name: 'Abrir archivo' })).toBeVisible()
  await page.screenshot({ path: screenshotPath('after-create.png'), fullPage: true })
  await page.getByRole('button', { name: 'Guardar', exact: true }).click()
  await expect(page).toHaveURL(/\/data\/grades\/created\/?$/)
  await expect(page.getByRole('heading', { name: 'Algorithms', exact: true })).toBeVisible()
  expect(
    requests.find((request) => request.method === 'records.create')?.params.values,
  ).toMatchObject({ grade: 19.5, tuition: 500.5, passed: true, tags: ['Core'] })
  await expect(page.locator('.data-file-preview iframe')).toBeVisible()
  await page.getByRole('link', { name: 'Editar registro' }).click()
  await page.getByLabel('Grade', { exact: true }).fill('20')
  await page.getByRole('button', { name: 'Guardar', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Algorithms', exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Eliminar registro', exact: true }).click()
  await expect(page.getByRole('dialog')).toContainText('Algorithms')
  expect(requests.some((request) => request.method === 'records.delete')).toBe(false)
  await page
    .getByRole('dialog')
    .getByRole('button', { name: 'Eliminar registro', exact: true })
    .click()
  await expect(page).toHaveURL(/\/data\/grades\/?$/)
  await expect(page.getByRole('cell', { name: 'Algorithms', exact: true })).toHaveCount(0)
})
test('filter, sort, column picker and both export formats use typed parameters', async ({
  page,
}) => {
  const requests = await mockData(page)
  await page.goto('/data/grades')
  await page.getByRole('button', { name: 'Añadir filtro' }).click()
  await page.getByLabel('Campo', { exact: true }).selectOption('grade')
  await page.getByLabel('Operador').selectOption('gte')
  await page.getByLabel('Valor', { exact: true }).fill('17')
  await expect(page.getByRole('cell', { name: 'Mathematics', exact: true })).toBeVisible()
  await expect(page.getByRole('cell', { name: 'Physics', exact: true })).toHaveCount(0)
  await page.getByRole('button', { name: 'Ordenar por Grade' }).click()
  await expect(page.getByRole('columnheader', { name: /Grade/ })).toHaveAttribute(
    'aria-sort',
    'ascending',
  )
  await page.getByText('Columnas', { exact: true }).click()
  await page.locator('.data-columns').getByLabel('Teacher email').uncheck()
  await expect(page.getByRole('columnheader', { name: /Teacher email/ })).toHaveCount(0)
  for (const format of ['CSV', 'XLSX']) {
    const download = page.waitForEvent('download')
    await page.getByRole('button', { name: `Exportar ${format}` }).click()
    expect((await download).suggestedFilename()).toBe(`grades.${format.toLowerCase()}`)
  }
  expect(
    requests
      .filter((request) => request.method === 'collections.export')
      .every(
        (request) =>
          JSON.stringify(request.params.filters) ===
          JSON.stringify([{ field: 'grade', op: 'gte', value: 17 }]),
      ),
  ).toBe(true)
})
test('390 px layouts in both themes and English fit without horizontal overflow', async ({
  page,
}) => {
  await mockData(page)
  await page.setViewportSize({ width: 390, height: 844 })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.addInitScript(() => localStorage.setItem('excalibur-ui-language', 'en'))
  for (const theme of ['light', 'dark']) {
    for (const path of [
      '/data',
      '/data/grades',
      '/data/grades/new',
      '/data/grades/math',
      '/data/grades/math/edit',
    ]) {
      await page.goto(path)
      await page.evaluate(
        (value) => document.documentElement.classList.toggle('dark', value === 'dark'),
        theme,
      )
      await expect(page.locator('.data-page h1')).toBeVisible()
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
        true,
      )
      await page.screenshot({
        path: screenshotPath(`after-mobile-${theme}-${path.replaceAll('/', '-')}.png`),
        fullPage: true,
      })
    }
  }
})
test('unsaved edit can be kept or discarded through an accessible dialog', async ({ page }) => {
  await mockData(page)
  await page.goto('/data/grades/math/edit')
  await page.getByLabel('Grade', { exact: true }).fill('12')
  await page.getByRole('button', { name: 'Cancelar', exact: true }).click()
  await expect(page.getByRole('dialog')).toContainText('cambios sin guardar')
  await page.getByRole('dialog').getByRole('button', { name: 'Cancelar', exact: true }).click()
  await expect(page.getByLabel('Grade', { exact: true })).toHaveValue('12')
  await page.getByRole('button', { name: 'Cancelar', exact: true }).click()
  await page.getByRole('button', { name: 'Descartar cambios', exact: true }).click()
  await expect(page).toHaveURL(/\/data\/grades\/?$/)
})
test('new collection and file import open chat drafts without sending or creating', async ({
  page,
}) => {
  const requests = await mockData(page)
  await page.goto('/data')
  await page.getByRole('button', { name: 'Nueva colección' }).click()
  await expect(page).toHaveURL(/\/agents\/excalibur/)
  await expect(page.locator('textarea')).toHaveValue(/Propón primero la estructura/)
  await page.goto('/data')
  await page.getByLabel('Importar Excel/CSV', { exact: true }).setInputFiles({
    name: 'grades.csv',
    mimeType: 'text/csv',
    buffer: Buffer.from('Course,Grade\nAlgorithms,19'),
  })
  await expect(page).toHaveURL(/\/agents\/excalibur/)
  await expect(page.locator('textarea')).toHaveValue(/grades.csv.*blob/)
  await expect(page.locator('.library-attachments').first()).toContainText('grades.csv')
  expect(
    requests.some((request) =>
      ['collections.create', 'collections.import'].includes(request.method),
    ),
  ).toBe(false)
})

test('live collection changes refresh open pages and the sidebar', async ({ page }) => {
  await mockData(page)
  const events = await mockEvents(page)
  await page.goto('/data/grades')
  await events.ready('collections.changed')
  await expect(page.getByRole('heading', { name: /University grades/ })).toBeVisible()
  await page.route('**/rpc', async (route) => {
    const { id, method } = route.request().postDataJSON()
    const collection = { ...exampleCollection, name: 'Semester grades', record_count: 3 }
    const result =
      method === 'collections.list'
        ? { collections: [collection] }
        : method === 'collections.get'
          ? { collection }
          : method === 'records.list'
            ? {
                records: [
                  ...exampleRecords,
                  {
                    ...exampleRecords[0],
                    id: 'algorithms',
                    values: { course: 'Algorithms', grade: 19 },
                  },
                ],
                total: 3,
              }
            : undefined
    if (!result) return route.fallback()
    await route.fulfill({ json: { jsonrpc: '2.0', id, result } })
  })
  events.emit('collections.changed', {
    collection: 'grades',
    record_id: 'algorithms',
    kind: 'created',
  })
  await expect(page.getByRole('heading', { name: /Semester grades/ })).toBeVisible()
  await expect(page.getByRole('cell', { name: 'Algorithms', exact: true })).toBeVisible()
  await expect(
    page.locator('.sidebar').getByRole('button', { name: /Semester grades/ }),
  ).toContainText('3')
})
