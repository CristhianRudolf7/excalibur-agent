import { readFile } from 'node:fs/promises'
import { test, expect, type Page } from './fixtures'
import { mockEvents } from './event-fixture'
import type { Artifact } from '../src/features/artifacts/types'
import { seedPDF } from '../src/features/viewer/seeds'

const session_id = 'cnv_web_excalibur'
const overview: Artifact = {
  id: 'overview',
  title: 'System overview',
  kind: 'diagram',
  format: 'svg',
  mime: 'image/svg+xml',
  size: 300,
  created_at: '2026-11-02T10:00:00Z',
  dialect: 'Mermaid',
  group: 'system',
}
const detail: Artifact = {
  ...overview,
  id: 'detail',
  title: 'Storage detail',
  created_at: '2026-11-02T10:01:00Z',
}
const svg =
  '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1200 800"><script>window.artifactScriptRan=true</script><foreignObject width="10" height="10"><div>unsafe</div></foreignObject><rect width="1200" height="800" fill="white" onclick="alert(1)"/><path d="M20 20L1180 780" stroke="black"/><use href="https://invalid.example/remote.svg#x"/><text x="40" y="80" fill="black">Architecture</text></svg>'
const source = 'graph TD; Client-->Gateway; Gateway-->Store'
async function mockArtifacts(page: Page) {
  await page.addInitScript(() => {
    localStorage.setItem('excalibur-ui-language', 'en')
  })
  const bus = await mockEvents(page)
  await page.route('**/rpc', async (route) => {
    const request = route.request().postDataJSON()
    if (request.method !== 'artifacts.get') return route.fallback()
    const id = request.params.id
    const result =
      id === 'pdf'
        ? { id, mime: 'application/pdf', content: Buffer.from(seedPDF()).toString('base64') }
        : id === 'png'
          ? {
              id,
              mime: 'image/png',
              content:
                'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII=',
            }
          : { id, mime: 'image/svg+xml', content: svg, source, dialect: 'Mermaid' }
    await route.fulfill({ json: { jsonrpc: '2.0', id: request.id, result } })
  })
  return bus
}
for (const width of [1440, 390]) {
  test(`visual events, grouped previews, exports and conversation history at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: width === 390 ? 844 : 1000 })
    const bus = await mockArtifacts(page)
    await page.goto('/agents/excalibur?view=chat')
    await bus.ready('artifact.ready')
    bus.emit('artifact.ready', { session_id: 'other', turn_id: 'wrong', artifacts: [overview] })
    bus.emit('artifact.created', { session_id, turn_id: 'one', artifact: overview })
    await expect(page.locator('.right-panel')).toHaveCount(0)
    await page.getByRole('button', { name: 'Toggle panel', exact: true }).click()
    const card = page.locator('.panel-launcher').filter({ hasText: 'Diagrams' })
    await expect(card.getByRole('status')).toHaveText('1 diagram')
    bus.emit('artifact.created', { session_id, turn_id: 'one', artifact: detail })
    await expect(card.getByRole('status')).toHaveText('2 diagrams')
    await expect(card.locator('.activity-dot')).toBeVisible()
    bus.emit('artifact.ready', { session_id, turn_id: 'one', artifacts: [overview, detail] })
    await expect(page).toHaveURL(/panel=diagrams/)
    const panel = page.locator('#panel-diagrams')
    await expect(panel.getByRole('img', { name: overview.title })).toBeVisible()
    await expect
      .poll(() =>
        panel.getByRole('img').evaluate((node) => (node as HTMLImageElement).naturalWidth),
      )
      .toBeGreaterThan(0)
    const safe = await panel
      .getByRole('img')
      .evaluate(async (image) => (await fetch((image as HTMLImageElement).src)).text())
    expect(safe).not.toMatch(/script|foreignObject|onclick|https:/)
    await panel.getByRole('button', { name: 'Zoom in', exact: true }).click()
    expect(await panel.getByRole('img').getAttribute('style')).toContain('125%')
    const viewport = panel.locator('.viewer-image-viewport')
    await viewport.scrollIntoViewIfNeeded()
    const box = (await viewport.boundingBox())!
    await page.mouse.move(box.x + box.width * 0.6, box.y + 30)
    await page.mouse.down()
    await page.mouse.move(box.x + box.width * 0.2, box.y + 30, { steps: 4 })
    await page.mouse.up()
    expect(await viewport.evaluate((node) => node.scrollLeft)).toBeGreaterThan(0)
    await panel.getByRole('button', { name: 'View source · Mermaid' }).click()
    await expect(panel.locator('pre')).toHaveText(source)
    const svgDownload = page.waitForEvent('download')
    await panel.getByRole('link', { name: 'Download SVG' }).click()
    const savedSVG = await svgDownload
    expect(savedSVG.suggestedFilename()).toBe('System-overview.svg')
    expect(await readFile((await savedSVG.path())!, 'utf8')).not.toMatch(
      /script|foreignObject|onclick|https:/,
    )
    const pngDownload = page.waitForEvent('download')
    await panel.getByRole('button', { name: 'Download PNG' }).click()
    const savedPNG = await pngDownload
    expect(savedPNG.suggestedFilename()).toBe('System-overview.png')
    expect((await readFile((await savedPNG.path())!)).subarray(1, 4).toString()).toBe('PNG')
    await panel.getByRole('button', { name: detail.title, exact: true }).click()
    await expect(panel.getByRole('img', { name: detail.title })).toBeVisible()
    await page.screenshot({ path: test.info().outputPath(`diagrams-${width}.png`) })
    await page
      .locator('.panel-navigation')
      .getByRole('button', { name: 'Close', exact: true })
      .click()
    bus.emit('artifact.ready', { session_id, turn_id: 'one', artifacts: [overview, detail] })
    await expect(page.locator('.right-panel')).toHaveCount(0)
    await page.reload()
    await bus.ready('artifact.ready')
    bus.emit('artifact.ready', { session_id, turn_id: 'one', artifacts: [overview, detail] })
    await expect(page.locator('.right-panel')).toHaveCount(0)
    await page.getByRole('button', { name: 'Toggle panel', exact: true }).click()
    // Reopening the home card leaves the turn filter and exposes all conversation metadata.
    if (await page.getByRole('button', { name: 'Back to panel home' }).count())
      await page.getByRole('button', { name: 'Back to panel home' }).click()
    await card.click()
    await expect(panel.getByRole('option').first()).toHaveText('Storage detailSVG')
    await expect(panel.getByRole('option')).toHaveCount(2)
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  })
}
test('the general setting disables automatic opening while retaining visuals for later', async ({
  page,
}) => {
  const bus = await mockArtifacts(page)
  await page.goto('/settings?section=general')
  await page
    .getByRole('checkbox', { name: 'Automatically open diagrams when each turn finishes' })
    .uncheck()
  await page.goto('/agents/excalibur?view=chat')
  await bus.ready('artifact.ready')
  bus.emit('artifact.ready', { session_id, turn_id: 'one', artifacts: [overview] })
  await page.getByRole('button', { name: 'Toggle panel', exact: true }).click()
  const card = page.locator('.panel-launcher').filter({ hasText: 'Diagrams' })
  await expect(card).toContainText('System overview')
  await expect(page).not.toHaveURL(/panel=diagrams/)
  await card.click()
  await expect(page.getByRole('img', { name: overview.title })).toBeVisible()
  await page.reload()
  await bus.ready('artifact.ready')
  bus.emit('artifact.ready', { session_id, turn_id: 'two', artifacts: [detail] })
  await expect(page.locator('.right-panel')).toHaveCount(0)
})
test('PNG and PDF artifacts reuse the image and PDF viewers', async ({ page }) => {
  const bus = await mockArtifacts(page)
  await page.goto('/agents/excalibur?view=chat')
  await bus.ready('artifact.ready')
  const png: Artifact = {
    ...overview,
    id: 'png',
    title: 'Chart image',
    kind: 'chart',
    format: 'png',
    mime: 'image/png',
    group: undefined,
  }
  bus.emit('artifact.ready', { session_id, turn_id: 'png-turn', artifacts: [png] })
  await expect(page.getByRole('img', { name: png.title })).toBeVisible()
  await expect
    .poll(() =>
      page
        .getByRole('img', { name: png.title })
        .evaluate((node) => (node as HTMLImageElement).naturalWidth),
    )
    .toBe(1)
  const pdf: Artifact = {
    ...overview,
    id: 'pdf',
    title: 'Report PDF',
    kind: 'document',
    format: 'pdf',
    mime: 'application/pdf',
    group: undefined,
  }
  bus.emit('artifact.ready', { session_id, turn_id: 'pdf-turn', artifacts: [pdf] })
  await expect(page.locator('.artifact-preview h3')).toHaveText(pdf.title)
  await expect
    .poll(() => page.locator('.artifact-preview canvas').evaluate((node) => node.width))
    .toBeGreaterThan(0)
  await page.getByRole('button', { name: 'Next page', exact: true }).click()
  await expect(page.locator('.artifact-preview canvas')).toHaveAttribute('aria-label', 'Page 2')
  await expect(page.getByRole('link', { name: 'Download PDF' })).toHaveAttribute(
    'download',
    'Report-PDF.pdf',
  )
})

test('long conversation histories are virtualized and navigable with the keyboard', async ({
  page,
}) => {
  const bus = await mockArtifacts(page)
  await page.goto('/agents/excalibur?view=chat')
  await bus.ready('artifact.ready')
  const artifacts = Array.from({ length: 60 }, (_, index) => ({
    ...overview,
    id: `diagram-${index}`,
    title: `Diagram ${index}`,
    group: undefined,
    created_at: `2026-11-02T10:${String(index).padStart(2, '0')}:00Z`,
  }))
  bus.emit('artifact.ready', { session_id, turn_id: 'many', artifacts })
  const history = page.getByRole('listbox', { name: 'Conversation diagrams' })
  await expect(history.getByRole('option').first()).toHaveText('Diagram 59SVG')
  expect(await history.getByRole('option').count()).toBeLessThan(60)
  await history.focus()
  await history.press('End')
  await expect(page.getByRole('img', { name: 'Diagram 0', exact: true })).toBeVisible()
  await expect(history.getByRole('option', { name: 'Diagram 0 SVG', exact: true })).toHaveAttribute(
    'aria-selected',
    'true',
  )
  await history.press('Home')
  await expect(page.getByRole('img', { name: 'Diagram 59', exact: true })).toBeVisible()
})
test('opens a sanitized SVG as an image layer in the existing conversation whiteboard', async ({
  page,
}) => {
  const bus = await mockArtifacts(page)
  const board = {
    id: 'board',
    conversation_id: session_id,
    title: 'Architecture board',
    version: 1,
    updated_at: '',
    elements: [
      { id: 'original', type: 'rect', x: 0, y: 0, width: 100, height: 100, author: 'user' },
    ],
  }
  let imported: { type: string; src: string } | undefined
  await page.route('**/rpc', async (route) => {
    const request = route.request().postDataJSON()
    if (!request.method.startsWith('whiteboard.')) return route.fallback()
    if (request.method === 'whiteboard.apply') {
      imported = request.params.ops[0].elements[0]
      board.elements.push(...request.params.ops[0].elements)
      board.version++
    }
    const result =
      request.method === 'whiteboard.list'
        ? { boards: [board] }
        : request.method === 'whiteboard.apply'
          ? { version: board.version }
          : board
    await route.fulfill({ json: { jsonrpc: '2.0', id: request.id, result } })
  })
  await page.goto('/agents/excalibur?view=chat')
  await bus.ready('whiteboard.opened')
  await bus.ready('artifact.ready')
  bus.emit('whiteboard.opened', { conversation_id: session_id, board })
  await expect(page).toHaveURL(/panel=whiteboard/)
  bus.emit('artifact.ready', { session_id, turn_id: 'one', artifacts: [overview] })
  await page.getByRole('button', { name: 'Open in whiteboard', exact: true }).click()
  await expect(page).toHaveURL(/panel=whiteboard/)
  await expect(page.locator('.excalidraw')).toBeVisible()
  await expect(page.getByTestId('whiteboard-surface')).toHaveAttribute(
    'data-elements',
    /"type":"image"/,
  )
  await expect(page.getByTestId('whiteboard-surface')).toHaveAttribute('data-elements', /original/)
  await expect(page.locator('.board-error')).toHaveCount(0)
  expect(imported?.type).toBe('image')
  expect(imported?.src).toMatch(/^data:image\/svg\+xml;charset=utf-8,/)
  expect(decodeURIComponent(imported!.src)).not.toMatch(/script|foreignObject|onclick|https:/)
  expect(board.elements).toHaveLength(2)
})
