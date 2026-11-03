import { expect, test } from './fixtures'
import { seedDocument, seedPages, seedChunk, seedPDF } from '../src/features/viewer/seeds'

for (const mobile of [false, true]) {
  test(`document previews and citations ${mobile ? 'mobile' : 'desktop'}`, async ({ page }) => {
    await page.setViewportSize(mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 })
    await page.route('**/rpc', async (route) => {
      const request = route.request().postDataJSON() as { id: string; method: string }
      if (!request.method.startsWith('library.')) return route.fallback()
      await route.fulfill({
        json: {
          jsonrpc: '2.0',
          id: request.id,
          error: { code: -32601, message: 'Method unavailable' },
        },
      })
    })
    await page.goto('/agents/excalibur?panelDeepLink=1&panelDeepLink=1&panel=document&view=chat')
    await page.getByRole('button', { name: /Alternar panel|Toggle panel/ }).click()
    const panel = page.locator('.panel-content')
    await panel.getByRole('button', { name: 'DOCX', exact: true }).click()
    await expect(panel.locator('[data-highlight="true"]')).toContainText('Exact citations')
    await expect(panel.getByRole('table')).toBeVisible()
    await page.screenshot({
      path: test.info().outputPath(`document-${mobile ? 'mobile' : 'desktop'}.png`),
    })
    await panel.getByRole('button', { name: /Cerrar documento|Close document/ }).click()
    // Reopening the panel lands on its home launcher; a fresh deep link shows the viewer again.
    await page.goto('/agents/excalibur?panelDeepLink=1&panelDeepLink=1&panel=document&view=chat')
    await page.getByRole('button', { name: /Alternar panel|Toggle panel/ }).click()
    await panel.getByRole('button', { name: 'XLSX', exact: true }).click()
    await expect(panel.getByRole('gridcell', { name: 'B2' })).toHaveAttribute(
      'aria-selected',
      'true',
    )
    await expect(panel.getByRole('gridcell', { name: 'B2' })).toHaveText('42')
    await panel.getByRole('button', { name: /Cerrar documento|Close document/ }).click()
    await page.goto('/agents/excalibur?panelDeepLink=1&panelDeepLink=1&panel=document&view=chat')
    await page.getByRole('button', { name: /Alternar panel|Toggle panel/ }).click()
    await panel.getByRole('button', { name: 'PPTX', exact: true }).click()
    const slide = await panel.locator('.viewer-slide').boundingBox()
    expect(slide!.width / slide!.height).toBeCloseTo(16 / 9, 1)
    await panel.getByRole('button', { name: /Notas|Notes/ }).click()
    await expect(panel.getByText('Keep the original document for reference.')).toBeVisible()
    await panel.getByRole('tab', { name: 'Overview 2' }).click()
    await expect(panel.getByRole('heading', { name: 'Document library 2' })).toBeVisible()
    await panel.getByRole('button', { name: /Cerrar documento|Close document/ }).click()
    await page.goto('/agents/excalibur?panelDeepLink=1&panelDeepLink=1&panel=document&view=chat')
    await page.getByRole('button', { name: /Alternar panel|Toggle panel/ }).click()
    await panel.getByRole('button', { name: 'PDF', exact: true }).click()
    await expect(panel.locator('canvas')).toBeVisible()
    await expect(panel.locator('.viewer-pdf-highlight')).toBeVisible({ timeout: 20000 })
    const box = await panel.locator('.viewer-pdf-highlight').boundingBox()
    const canvas = await panel.locator('canvas').boundingBox()
    expect(box!.width / canvas!.width).toBeCloseTo(460 / 612, 2)
    await panel.getByRole('button', { name: /Ampliar|Zoom in/ }).click()
    await expect(panel.locator('.viewer-pdf-highlight')).toBeVisible()
    await page.screenshot({
      path: test.info().outputPath(`pdf-${mobile ? 'mobile' : 'desktop'}.png`),
    })
  })
}

test('renders the original blob at the cited PDF page', async ({ page }) => {
  const digest = 'a'.repeat(64)
  const document = { ...seedDocument('demo-pdf'), blob_digest: digest, title: 'Original report' }
  const chunk = {
    ...seedChunk('demo-pdf', 'demo-citation'),
    page: 2,
    boxes: [{ page: 2, x0: 40, y0: 90, x1: 500, y1: 120 }],
  }
  await page.route('**/rpc', async (route) => {
    const request = route.request().postDataJSON() as {
      id: string
      method: string
      params: { from: number; to: number }
    }
    if (!request.method.startsWith('library.')) return route.fallback()
    const result =
      request.method === 'library.get'
        ? { document }
        : request.method === 'library.chunk'
          ? { chunk }
          : { document, pages: seedPages('demo-pdf') }
    if (request.method === 'library.pages')
      expect(request.params.to - request.params.from + 1).toBeLessThanOrEqual(20)
    await route.fulfill({ json: { jsonrpc: '2.0', id: request.id, result } })
  })
  let blobRequested = false
  await page.route(`**/blobs/${digest}`, async (route) => {
    blobRequested = true
    await route.fulfill({ body: Buffer.from(seedPDF()), contentType: 'application/pdf' })
  })
  await page.goto('/agents/excalibur?panelDeepLink=1&panelDeepLink=1&panel=document&view=chat')
  await page.getByRole('button', { name: /Alternar panel|Toggle panel/ }).click()
  await page.getByRole('button', { name: 'PDF', exact: true }).click()
  const panel = page.locator('.panel-content')
  await expect(page).toHaveURL(/agents\/excalibur/)
  await expect(panel.getByRole('spinbutton')).toHaveValue('2')
  await expect(panel.locator('.viewer-pdf-highlight')).toBeVisible()
  expect(blobRequested).toBe(true)
  await expect(
    panel.getByRole('link', { name: /Descargar original|Download original/ }),
  ).toHaveAttribute('href', `/blobs/${digest}`)
  await expect(panel.getByText(/Vista de ejemplo|Sample preview/)).toHaveCount(0)
})
