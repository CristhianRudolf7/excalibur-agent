import { test, expect } from '@playwright/test'
import { readFileSync } from 'node:fs'
import { seedDocument, seedPages } from '../src/features/viewer/seeds'

const digest = 'a'.repeat(64)
const document = {
  ...seedDocument('real-pdf'),
  id: 'real-pdf',
  format: 'pdf',
  mime: 'application/pdf',
  blob_digest: digest,
  title: 'Original report',
}
for (const mobile of [false, true]) {
  test(`opens real workspace contents in their viewers ${mobile ? 'mobile' : 'desktop'}`, async ({
    page,
  }) => {
    await page.setViewportSize(mobile ? { width: 390, height: 844 } : { width: 1440, height: 900 })
    await page.route('**/rpc', async (route) => {
      const { id, method, params } = route.request().postDataJSON()
      let result: unknown
      if (method === 'files.open') {
        const common = { path: params.path, size: 100, mime: 'application/octet-stream' }
        result =
          params.path === 'real.ts'
            ? { ...common, kind: 'text', content: 'export const realWorkspaceValue = 42;' }
            : params.path === 'budget.xlsx'
              ? { ...common, kind: 'sheet', document_id: 'workbook' }
              : params.path === 'report.pdf'
                ? { ...common, kind: 'document', document_id: 'real-pdf' }
                : params.path === 'photo.png'
                  ? { ...common, kind: 'image', blob_url: `/blobs/${'b'.repeat(64)}` }
                  : { ...common, kind: 'binary' }
        if (params.path === 'missing.pdf')
          return route.fulfill({
            json: { jsonrpc: '2.0', id, error: { code: -32000, message: 'outside workspace' } },
          })
      } else if (method === 'library.get')
        result = {
          document:
            params.id === 'workbook'
              ? { ...document, id: 'workbook', format: 'xlsx', page_count: 2 }
              : document,
        }
      else if (method === 'library.pages')
        result = {
          document,
          pages:
            params.id === 'workbook' ? seedPages('demo-xlsx').slice(0, 2) : seedPages('demo-pdf'),
        }
      else return route.fulfill({ json: { jsonrpc: '2.0', id, result: {} } })
      await route.fulfill({ json: { jsonrpc: '2.0', id, result } })
    })
    await page.route(`**/blobs/${digest}`, (route) =>
      route.fulfill({
        body: readFileSync('e2e/fixtures/report.pdf'),
        contentType: 'application/pdf',
      }),
    )
    await page.goto('/e2e/fixtures/open-files.html')
    await page.getByRole('button', { name: 'real.ts', exact: true }).click()
    await expect(page.locator('.monaco-editor')).toContainText('realWorkspaceValue', {
      timeout: 30000,
    })
    await expect(page).toHaveURL(/panel=files/)
    await page.getByRole('button', { name: 'budget.xlsx', exact: true }).click()
    await expect(page.getByRole('gridcell', { name: 'Cell B2', exact: true })).toHaveText('42')
    await expect(page.getByRole('tab', { name: 'Sheet1' })).toBeVisible()
    await expect(page).toHaveURL(/panel=sheet/)
    await page.getByRole('button', { name: 'report.pdf', exact: true }).click()
    const canvas = page.locator('.viewer-pdf canvas')
    await expect(canvas).toBeVisible()
    await expect
      .poll(() => canvas.evaluate((node: HTMLCanvasElement) => node.width))
      .toBeGreaterThan(300)
    // White-only canvases do not prove the PDF rendered: inspect actual ink pixels.
    await expect
      .poll(() =>
        canvas.evaluate((node: HTMLCanvasElement) => {
          const pixels = node.getContext('2d')!.getImageData(0, 0, node.width, node.height).data
          let ink = 0
          for (let i = 0; i < pixels.length; i += 4)
            if (pixels[i]! < 100 && pixels[i + 3]! > 0) ink++
          return ink
        }),
      )
      .toBeGreaterThan(100)
    const before = await canvas.evaluate((node: HTMLCanvasElement) => node.width)
    await page.getByRole('button', { name: 'Zoom in', exact: true }).click()
    await expect
      .poll(() => canvas.evaluate((node: HTMLCanvasElement) => node.width))
      .toBeGreaterThan(before)
    await page.getByRole('button', { name: 'Fit width' }).click()
    await page.getByRole('button', { name: 'Next', exact: true }).click()
    await expect(page.getByRole('spinbutton')).toHaveValue('2')
    await expect(canvas).toBeVisible()
    await page.screenshot({
      path: `test-results/rp-shots/fe-b-pdf-${mobile ? 'mobile' : 'desktop'}.png`,
    })
    if (!mobile) {
      await page.evaluate(() => document.documentElement.classList.add('dark'))
      await page.screenshot({
        path: 'test-results/rp-shots/fe-b-pdf-dark.png',
      })
    }
    await page.getByRole('button', { name: 'archive.bin', exact: true }).click()
    await expect(page.getByRole('heading', { name: 'Preview unavailable' })).toBeVisible()
    await page.getByRole('button', { name: 'missing.pdf', exact: true }).click()
    await expect(
      page.getByRole('region', { name: 'Document viewer' }).getByRole('alert'),
    ).toContainText('Could not open this file')
    await expect(page.getByRole('button', { name: 'Retry', exact: true })).toBeVisible()
  })
}

test('renders PDF bytes even while extraction is pending and has no pages', async ({ page }) => {
  let reads = 0
  await page.route('**/rpc', async (route) => {
    const { id, method } = route.request().postDataJSON()
    const pending = { ...document, format: '', status: 'pending', page_count: 0 }
    const result =
      method === 'files.open'
        ? {
            path: 'report.pdf',
            kind: 'document',
            document_id: 'real-pdf',
            mime: 'application/pdf',
            size: 100,
          }
        : method === 'library.get'
          ? { document: ++reads > 1 ? document : pending }
          : { document: pending, pages: [] }
    await route.fulfill({ json: { jsonrpc: '2.0', id, result } })
  })
  await page.route(`**/blobs/${digest}`, (route) =>
    route.fulfill({
      body: readFileSync('e2e/fixtures/report.pdf'),
      contentType: 'application/pdf',
    }),
  )
  await page.goto('/e2e/fixtures/open-files.html')
  await page.getByRole('button', { name: 'report.pdf', exact: true }).click()
  await expect(page.locator('.viewer-pdf canvas')).toBeVisible()
  await expect(page.getByRole('spinbutton')).toHaveAttribute('max', '2')
  await expect.poll(() => reads).toBeGreaterThan(1)
  await expect(page.locator('.viewer-badge').first()).toHaveText('PDF')
})
