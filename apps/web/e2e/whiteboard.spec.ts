import { test, expect } from './fixtures'
import { mockEvents } from './event-fixture'
import type { Board, BoardEvent, Op } from '../src/features/whiteboard/types'
import { applyOps } from '../src/features/whiteboard/scene'

test.use({ actionTimeout: 15000 })

for (const mobile of [false, true])
  test(`agent and user share a whiteboard ${mobile ? 'at 390px' : 'on desktop'}`, async ({
    page,
  }) => {
    test.setTimeout(90000)
    await page.setViewportSize(mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 })
    let board: Board = {
      id: 'board-1',
      conversation_id: 'cnv_web_excalibur',
      title: 'A shared idea',
      version: 0,
      elements: [],
      updated_at: '2026-11-02T00:00:00Z',
    }
    const events = await mockEvents(page)
    const applied: { board_id: string; base_version: number; ops: Op[]; client_op_id: string }[] =
      []
    const snapshots: { request_id: string; image_blob: string }[] = []
    let uploaded = false
    await page.route('**/rpc', async (route) => {
      const request = route.request().postDataJSON()
      if (!request.method.startsWith('whiteboard.')) return route.fallback()
      let result: unknown = {}
      if (request.method === 'whiteboard.list') result = { boards: [board] }
      if (request.method === 'whiteboard.get') result = board
      if (request.method === 'whiteboard.apply') {
        applied.push(request.params)
        board = {
          ...board,
          version: board.version + 1,
          elements: applyOps(board.elements, request.params.ops),
        }
        result = { version: board.version }
      }
      if (request.method === 'whiteboard.snapshot') {
        snapshots.push(request.params)
        result = { ok: true }
      }
      await route.fulfill({ json: { jsonrpc: '2.0', id: request.id, result } })
    })
    await page.route('**/blobs/*', async (route) => {
      const body = route.request().postDataBuffer()
      uploaded = body?.subarray(1, 4).toString() === 'PNG'
      await route.fulfill({ status: 201, json: { ok: true } })
    })
    const event = (type: string, payload: Partial<BoardEvent>) =>
      events.emit(type, {
        conversation_id: board.conversation_id,
        board_id: board.id,
        ...payload,
      })
    await page.goto('/agents/excalibur?view=chat')
    await events.ready('whiteboard.opened')
    event('whiteboard.opened', { board, layout: 'panel' })
    await expect(page).toHaveURL(/panel=whiteboard/)
    await expect(page.getByTestId('whiteboard-surface')).toBeVisible({ timeout: 45000 })
    await expect(page.getByTestId('toolbar-rectangle')).toBeVisible()
    const ops: Op[] = [
      {
        op: 'add',
        elements: [
          {
            id: 'agent-rect',
            type: 'rect',
            x: 50,
            y: 80,
            width: 120,
            height: 70,
            author: 'agent',
            style: { stroke: 'rgb(50, 60, 70)' },
          },
          {
            id: 'agent-text',
            type: 'text',
            x: 50,
            y: 180,
            width: 120,
            height: 30,
            text: 'A shared idea',
            author: 'agent',
          },
        ],
      },
    ]
    board = { ...board, version: 1, elements: applyOps(board.elements, ops) }
    event('whiteboard.ops', { version: 1, ops, animate: true, step_ms: 30 })
    await expect(page.getByTestId('whiteboard-surface')).toHaveAttribute('data-version', '1')
    await expect(page.getByTestId('whiteboard-surface')).toHaveAttribute(
      'data-elements',
      /agent-text/,
    )
    event('whiteboard.highlight', { ids: ['agent-rect'], style: 'laser', duration_ms: 10000 })
    await expect(page.getByTestId('board-highlight')).toHaveClass(/board-laser/)
    event('whiteboard.focus', { ids: ['agent-rect'], zoom: 1.5 })
    await expect(page.getByTestId('board-canvas')).toHaveAttribute('data-zoom', '1.5')
    await page.getByRole('button', { name: /Pantalla completa|Fullscreen/, exact: true }).click()
    await expect(page.getByRole('dialog', { name: /Pizarra|Whiteboard/ })).toBeVisible()
    await page.getByTestId('toolbar-rectangle').locator('..').click()
    const canvas = page.locator('.excalidraw__canvas.interactive')
    const box = await canvas.boundingBox()
    await page.mouse.move(box!.x + box!.width * 0.4, box!.y + box!.height * 0.4)
    await page.mouse.down()
    await page.mouse.move(box!.x + box!.width * 0.6, box!.y + box!.height * 0.6, { steps: 8 })
    await page.mouse.up()
    await expect.poll(() => applied.length).toBeGreaterThan(0)
    const edit = applied.find((batch) =>
      batch.ops.some((op) => op.elements?.some((element) => element.author === 'user')),
    )
    expect(edit).toMatchObject({ board_id: 'board-1', base_version: 1 })
    expect(edit?.client_op_id).toBeTruthy()
    event('whiteboard.snapshot_requested', { request_id: 'snapshot-1' })
    await expect.poll(() => snapshots.length).toBe(1)
    expect(uploaded).toBe(true)
    expect(snapshots[0]).toMatchObject({
      request_id: 'snapshot-1',
      image_blob: expect.stringMatching(/^[a-f0-9]{64}$/),
    })
    if (!mobile) {
      const illustrated: Op[] = [
        {
          op: 'add',
          elements: [
            {
              id: 'formula',
              type: 'math',
              x: 260,
              y: 80,
              width: 220,
              height: 80,
              latex: 'E = mc^2',
              author: 'agent',
            },
            {
              id: 'code',
              type: 'code',
              x: 260,
              y: 200,
              width: 280,
              height: 100,
              code: 'const answer = 42',
              language: 'javascript',
              author: 'agent',
            },
            {
              id: 'note',
              type: 'sticky',
              x: 550,
              y: 80,
              width: 180,
              height: 100,
              text: 'Try it together',
              author: 'agent',
            },
            {
              id: 'diagram',
              type: 'mermaid',
              x: 550,
              y: 240,
              mermaid: 'flowchart TD; A[Input] --> B[Output]',
              author: 'agent',
            },
          ],
        },
      ]
      board = {
        ...board,
        version: board.version + 1,
        elements: applyOps(board.elements, illustrated),
      }
      event('whiteboard.ops', { version: board.version, ops: illustrated })
      await expect(page.getByTestId('whiteboard-surface')).toHaveAttribute(
        'data-elements',
        /diagram:0/,
        { timeout: 30000 },
      )
      event('whiteboard.snapshot_requested', { request_id: 'snapshot-illustrated' })
      await expect.poll(() => snapshots.length, { timeout: 15000 }).toBe(2)
      expect(snapshots[1]?.request_id).toBe('snapshot-illustrated')
      expect(uploaded).toBe(true)
      for (const format of ['PNG', 'SVG', 'JSON']) {
        const [download] = await Promise.all([
          page.waitForEvent('download'),
          page
            .getByRole('button', {
              name: new RegExp(`Exportar ${format}|Export ${format}`),
              exact: true,
            })
            .click(),
        ])
        expect(download.suggestedFilename()).toBe(`whiteboard.${format.toLowerCase()}`)
      }
    }
    await page.screenshot({ path: test.info().outputPath('whiteboard.png') })
    await page.getByRole('button', { name: /Volver al panel|Return to panel/ }).click()
    await expect(page.getByRole('dialog', { name: /Pizarra|Whiteboard/ })).toHaveCount(0)
    await page
      .getByRole('button', { name: /Volver al inicio del panel|Back to panel home/ })
      .click()
    await page
      .locator('.panel-launcher')
      .filter({ hasText: /Archivos|Files/ })
      .click()
    event('whiteboard.opened', { board, layout: 'fullscreen' })
    await expect(page.getByRole('dialog', { name: /Pizarra|Whiteboard/ })).toBeVisible()
    await page.getByRole('button', { name: /Cerrar pizarra|Close whiteboard/ }).click()
    await expect(page.getByRole('dialog', { name: /Pizarra|Whiteboard/ })).toHaveCount(0)
  })

test('shows an honest offline state when the method is unavailable', async ({ page }) => {
  await page.route('**/rpc', async (route) => {
    const request = route.request().postDataJSON()
    if (!request.method.startsWith('whiteboard.')) return route.fallback()
    await route.fulfill({
      json: { jsonrpc: '2.0', id: request.id, error: { code: -32601, message: 'Unavailable' } },
    })
  })
  await page.goto('/agents/excalibur?panelDeepLink=1&panelDeepLink=1&panel=whiteboard&view=chat')
  await page.getByRole('button', { name: /Alternar panel|Toggle panel/ }).click()
  await expect(
    page
      .getByRole('status')
      .filter({ hasText: /pizarra no está disponible|Whiteboard unavailable/ }),
  ).toBeVisible()
  await expect(page.locator('.excalidraw')).toHaveCount(0)
})

for (const theme of ['light', 'dark'])
  test(`native chart labels and fills render in ${theme} mode`, async ({ page }, testInfo) => {
    const group = { author: 'agent' as const, group_id: 'expenses' }
    const board: Board = {
      id: 'chart-board',
      conversation_id: 'cnv_web_excalibur',
      title: 'Weekly expenses',
      version: 1,
      updated_at: '2026-11-04T00:00:00Z',
      elements: [
        {
          ...group,
          id: 'expenses',
          type: 'rect',
          x: 40,
          y: 80,
          width: 560,
          height: 340,
          style: { fill: '#ffffff', stroke: '#cbd5e1', roughness: 0 },
        },
        {
          ...group,
          id: 'expenses-title',
          type: 'text',
          x: 60,
          y: 94,
          width: 520,
          height: 27.5,
          text: 'Weekly expenses',
          style: { stroke: '#1e293b', font_size: 22, font_family: 'Helvetica' },
        },
        {
          ...group,
          id: 'expenses-bar-0',
          type: 'rect',
          x: 144,
          y: 141,
          width: 300,
          height: 22,
          style: { fill: '#4f86e8', stroke: '#4f86e8', roughness: 0 },
        },
        {
          ...group,
          id: 'expenses-label-0',
          type: 'text',
          x: 56,
          y: 138,
          width: 80,
          height: 20,
          text: 'Food',
          style: { stroke: '#1e293b', font_size: 16 },
        },
        {
          ...group,
          id: 'expenses-value-0',
          type: 'text',
          x: 454,
          y: 138,
          width: 100,
          height: 20,
          text: 'S/ 69.50',
          style: { stroke: '#1e293b', font_size: 16 },
        },
        {
          ...group,
          id: 'expenses-axis',
          type: 'line',
          x: 144,
          y: 136,
          width: 0,
          height: 248,
          points: [
            [0, 0],
            [0, 248],
          ],
          style: { stroke: '#64748b', roughness: 0 },
        },
      ],
    }
    const events = await mockEvents(page)
    await page.route('**/rpc', async (route) => {
      const request = route.request().postDataJSON()
      if (!request.method.startsWith('whiteboard.')) return route.fallback()
      await route.fulfill({
        json: {
          jsonrpc: '2.0',
          id: request.id,
          result: request.method === 'whiteboard.list' ? { boards: [board] } : board,
        },
      })
    })
    await page.setViewportSize({ width: 1440, height: 1000 })
    await page.goto('/agents/excalibur?view=chat')
    await events.ready('whiteboard.opened')
    await page.evaluate(
      (dark) => document.documentElement.classList.toggle('dark', dark),
      theme === 'dark',
    )
    events.emit('whiteboard.opened', {
      conversation_id: board.conversation_id,
      board_id: board.id,
      board,
      layout: 'fullscreen',
    })
    const surface = page.getByTestId('whiteboard-surface')
    await expect(surface).toBeVisible({ timeout: 45000 })
    await expect(surface).toHaveAttribute('data-elements', /expenses-value-0/)
    if (theme === 'dark') await expect(surface.locator('.excalidraw')).toHaveClass(/theme--dark/)
    else await expect(surface.locator('.excalidraw')).not.toHaveClass(/theme--dark/)
    // Native SVG export exercises Excalidraw's actual text and shape renderer.
    const download = page.waitForEvent('download')
    await page.getByRole('button', { name: /Export SVG|Exportar SVG/, exact: true }).click()
    const exported = await download
    const stream = await exported.createReadStream()
    const chunks: Buffer[] = []
    for await (const chunk of stream!) chunks.push(Buffer.from(chunk))
    const svg = Buffer.concat(chunks).toString()
    expect(svg).toContain('S/ 69.50')
    expect(svg).toContain('Food')
    expect(svg).toContain('Weekly expenses')
    expect(svg).toContain('#4f86e8')
    await surface.screenshot({ path: testInfo.outputPath(`chart-${theme}.png`) })
  })
