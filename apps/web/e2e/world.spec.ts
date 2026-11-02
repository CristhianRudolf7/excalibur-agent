import { mockChat } from './chat-fixture'
import { test, expect, type Page } from './fixtures'
import { clampCamera } from '../src/features/world/engine/camera'
import { timeline } from '../src/features/world/seed/timeline'
import { campus } from '../src/features/world/seed/layout'
const rooms = campus.rooms

const worldErrors = new WeakMap<Page, string[]>()
test.beforeEach(async ({ page }) => {
  const errors: string[] = []
  worldErrors.set(page, errors)
  page.on('pageerror', (error) => errors.push(error.message))
  page.on('console', (message) => {
    if (message.type() === 'error') errors.push(message.text())
  })
  await page.setViewportSize({ width: 1536, height: 1024 })
})
test.afterEach(async ({ page }) => {
  expect(worldErrors.get(page)).toEqual([])
})

async function worldReady(page: Page) {
  await expect(page.locator('.world-canvas')).toHaveAttribute('data-world-ready', 'true', {
    timeout: 30_000,
  })
  await expect.poll(() => page.evaluate(() => window.__excaliburWorld?.ready)).toBe(true)
  await settle(page)
}
async function settle(page: Page) {
  await page.evaluate(() => window.__excaliburWorld!.settle!())
}
async function advance(page: Page, milliseconds: number) {
  await page.evaluate((ms) => window.__excaliburWorld!.advance!(ms), milliseconds)
}
async function camera(page: Page) {
  return page.evaluate(() => window.__excaliburWorld!.camera!)
}
async function expectCamera(page: Page, expected: { x: number; y: number; zoom: number }) {
  await expect
    .poll(async () => {
      const current = await camera(page)
      return Math.max(
        Math.abs(current.x - expected.x),
        Math.abs(current.y - expected.y),
        Math.abs(current.zoom - expected.zoom),
      )
    })
    .toBeLessThan(0.1)
}
async function mapReady(page: Page) {
  await expect(page.getByRole('dialog', { name: 'Mapa del campus' })).toBeVisible()
  await page
    .locator('.world-map-modal')
    .evaluate((node) =>
      Promise.all(node.getAnimations({ subtree: true }).map((animation) => animation.finished)),
    )
  await expect
    .poll(async () => (await page.locator('.world-full-map-image').boundingBox())?.width ?? 0)
    .toBeGreaterThan(0)
}
async function openMap(page: Page) {
  await page.getByRole('button', { name: 'Abrir mapa completo', exact: true }).click()
  await mapReady(page)
}
async function visitMapRoom(page: Page, name: string) {
  await openMap(page)
  await page.locator('.world-map-room-list > button').filter({ hasText: name }).click()
  await expect(page.getByRole('dialog', { name: 'Mapa del campus' })).toBeHidden()
  await worldReady(page)
}
let emptyMapPoint: { x: number; y: number }
async function expectEmptyMapCamera(page: Page) {
  const box = (await page.locator('.world-canvas').boundingBox())!
  const current = await camera(page)
  await expectCamera(
    page,
    clampCamera(
      {
        x: emptyMapPoint.x - box.width / current.zoom / 2,
        y: emptyMapPoint.y - box.height / current.zoom / 2,
        zoom: current.zoom,
      },
      box.width,
      box.height,
      campus,
    ),
  )
}
async function clickEmptyMap(page: Page) {
  await mapReady(page)
  const surface = (await page.locator('.world-full-map-image').boundingBox())!
  emptyMapPoint = {
    x: (campus.width * (surface.width - 3)) / surface.width,
    y: (campus.height * (surface.height - 3)) / surface.height,
  }
  await page.mouse.click(surface.x + surface.width - 3, surface.y + surface.height - 3)
  await expect(page.getByRole('dialog', { name: 'Mapa del campus' })).toBeHidden()
  await worldReady(page)
}

test('mode control fits the chat header, centers over the world, restores the sidebar and stops Pixi', async ({
  page,
}) => {
  const errors: string[] = []
  page.on('pageerror', (error) => errors.push(error.message))
  await page.goto('/agents/excalibur?worldDebug=1&worldClock=manual')
  await page.getByRole('button', { name: 'Alternar panel', exact: true }).click()
  const control = page.getByRole('group', { name: 'Modo de vista' })
  const before = (await control.boundingBox())!
  const header = (await page.locator('.chat-header').boundingBox())!
  const main = (await page.locator('.main-panel').boundingBox())!
  const tabs = (await page.locator('.panel-navigation').boundingBox())!
  expect(before.x).toBeGreaterThanOrEqual(header.x)
  expect(before.x + before.width).toBeLessThanOrEqual(header.x + header.width)
  expect(before.y).toBeGreaterThanOrEqual(header.y)
  expect(before.y + before.height).toBeLessThanOrEqual(header.y + header.height)
  expect(Math.abs(header.y - main.y)).toBeLessThanOrEqual(1)
  expect(Math.abs(tabs.y - main.y)).toBeLessThanOrEqual(1)
  await control.getByRole('button', { name: 'Mundo 2D', exact: true }).click()
  await worldReady(page)
  const after = (await control.boundingBox())!
  const worldMain = (await page.locator('.main-panel').boundingBox())!
  expect(Math.abs(after.x + after.width / 2 - worldMain.x - worldMain.width / 2)).toBeLessThan(1)
  expect(after.y - worldMain.y).toBeLessThan(25)
  await expect(page.locator('.sidebar-rail')).toBeVisible()
  await page.keyboard.press('Control+j')
  // The Monaco editor in the side panel draws its own canvases; only the world canvas must be gone.
  await expect(page.locator('canvas:not(.monaco-editor canvas)')).toHaveCount(0)
  await expect.poll(() => page.evaluate(() => window.__excaliburWorld === undefined)).toBe(false)
  await expect.poll(() => page.evaluate(() => window.__excaliburWorld?.running)).toBe(false)
  await expect(page.locator('.sidebar')).toBeVisible()
  await page.keyboard.press('Control+j')
  await worldReady(page)
  await control.getByRole('button', { name: 'Chat', exact: true }).click()
  await expect(page.locator('canvas:not(.monaco-editor canvas)')).toHaveCount(0)
  expect(errors).toEqual([])
})
test('campus overlays, collapsed activity and persisted world mode', async ({ page }) => {
  await page.goto('/agents/excalibur?view=world&worldDebug=1&worldClock=manual')
  await worldReady(page)
  await expect(page.getByRole('region', { name: 'Tablero de misiones' })).toBeVisible()
  const activity = page.getByRole('region', { name: 'Actividad' })
  await expect(activity).toBeVisible()
  await expect(page.getByRole('region', { name: 'Mapa', exact: true })).toBeVisible()
  await expect(page.getByText('14:32')).toHaveCount(0)
  await expect(page.getByRole('button', { name: /Pausar|Redirigir|Conversar/ })).toHaveCount(0)
  await activity.getByRole('button').first().click()
  await expect(activity.getByRole('button').first()).toHaveAttribute('aria-expanded', 'false')
  await page.reload()
  await expect(activity.getByRole('button').first()).toHaveAttribute('aria-expanded', 'false')
  await activity.getByRole('button').first().click()
  await expect(activity.getByRole('button').first()).toHaveAttribute('aria-expanded', 'true')
})
test('measured room polygon opens an interior and Escape preserves the campus camera', async ({
  page,
}) => {
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.goto('/agents/excalibur?view=world&worldDebug=1&worldClock=manual')
  await worldReady(page)
  // The voice room is visible at the initial camera. Compute its center from the config.
  const polygon = rooms.find((room) => room.id === 'voice')!.polygon
  const center = {
    x: polygon.reduce((n, p) => n + p.x, 0) / polygon.length,
    y: polygon.reduce((n, p) => n + p.y, 0) / polygon.length,
  }
  const canvas = (await page.locator('canvas').boundingBox())!
  const { x: cameraX, y: cameraY, zoom } = await camera(page)
  await page.mouse.click(
    canvas.x + (center.x - cameraX) * zoom,
    canvas.y + (center.y - cameraY) * zoom,
  )
  await expect(page).toHaveURL(/room=voice/)
  await worldReady(page)
  await expect(page.locator('.world-back')).toBeVisible()
  await expect(page.locator('.world-minimap')).toHaveCount(0)
  await page.keyboard.press('Escape')
  await expect(page).not.toHaveURL(/room=/)
  await expect(page.locator('.world-minimap')).toBeVisible()
  await visitMapRoom(page, 'Biblioteca')
  await expect(page).toHaveURL(/room=library/)
  await page.locator('.world-back').click()
  await expect(page).not.toHaveURL(/room=/)
})
test('mock dialogs have portraits, advance, and share Enter bar messages with chat', async ({
  page,
}) => {
  const chat = await mockChat(page)
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.goto('/agents/excalibur?view=world&worldDebug=1&worldClock=manual&worldSpeed=0.25')
  await worldReady(page)
  await expect.poll(() => chat.connected()).toBe(true)
  chat.emit('message.created', {
    message_id: 'world-welcome',
    conversation_id: 'cnv_web_excalibur',
    role: 'assistant',
    text: 'Welcome to the live world conversation.',
  })
  const dialog = page.locator('.world-dialog')
  await expect(dialog).toBeVisible()
  await expect(dialog.locator('.pixel-avatar')).toBeVisible()
  // Drain any messages that arrived during renderer startup before testing the global Enter shortcut.
  for (let index = 0; index < 6 && (await dialog.count()); index++)
    await dialog.getByRole('button').click()
  await expect(dialog).toHaveCount(0)
  await page.keyboard.press('Enter')
  const input = page.locator('.world-enter input')
  await expect(input).toBeFocused()
  await input.fill('World message shared with chat')
  await input.press('Enter')
  chat.emit('message.created', {
    message_id: 'world-reply',
    conversation_id: 'cnv_web_excalibur',
    role: 'assistant',
    text: 'Message received from the world.',
  })
  await expect(dialog).toBeVisible()
  await page
    .getByRole('group', { name: 'Modo de vista' })
    .getByRole('button', { name: 'Chat', exact: true })
    .click()
  await expect(page.getByText('World message shared with chat', { exact: true })).toBeVisible()
})
test('room deliverables open the matching file or sheet panel', async ({ page }) => {
  await page.goto('/agents/atlas?view=world&room=finance&worldDebug=1&worldClock=manual')
  await worldReady(page)
  await page.locator('.world-deliverable').click()
  await page.locator('.world-open').click()
  await expect(page).toHaveURL(/panel=sheet/)
  await expect(page.getByRole('grid', { name: 'Hoja de cálculo' })).toBeVisible()
  await expect(page.locator('canvas')).toHaveCount(0)
})
test('mobile world fits 390 px and keeps rooms, activity and messaging usable', async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.goto('/agents/excalibur?view=world&worldDebug=1&worldClock=manual')
  await worldReady(page)
  await expect(page.locator('.world-minimap')).toBeHidden()
  await expect(page.locator('.world-enter input')).toBeVisible()
  await expect(page.locator('.world-missions')).toBeVisible()
  await visitMapRoom(page, 'Código')
  await expect(page.locator('.world-back')).toBeVisible()
  await page.locator('.world-back').click()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
})

test('ticker stops when hidden or outside the viewport and respects reduced motion', async ({
  page,
}) => {
  await page.goto('/agents/excalibur?view=world&worldDebug=1&worldClock=manual')
  await worldReady(page)
  await expect.poll(() => page.evaluate(() => window.__excaliburWorld?.running)).toBe(true)
  await page.evaluate(() => {
    Object.defineProperty(document, 'hidden', { configurable: true, get: () => true })
    document.dispatchEvent(new Event('visibilitychange'))
  })
  await expect.poll(() => page.evaluate(() => window.__excaliburWorld?.running)).toBe(false)
  await page.evaluate(() => {
    Object.defineProperty(document, 'hidden', { configurable: true, get: () => false })
    document.dispatchEvent(new Event('visibilitychange'))
  })
  await expect.poll(() => page.evaluate(() => window.__excaliburWorld?.running)).toBe(true)
  await page.locator('.world-canvas').evaluate((node: HTMLElement) => {
    node.style.transform = 'translateY(200vh)'
  })
  await expect.poll(() => page.evaluate(() => window.__excaliburWorld?.running)).toBe(false)
  await page.locator('.world-canvas').evaluate((node: HTMLElement) => {
    node.style.transform = ''
  })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await expect.poll(() => page.evaluate(() => window.__excaliburWorld?.running)).toBe(false)
})

test('minimap, wheel zoom and room double-click move the bounded camera', async ({ page }) => {
  await page.goto('/agents/excalibur?view=world&worldDebug=1&worldClock=manual')
  await worldReady(page)
  const initial = await camera(page)
  const map = page.locator('.world-minimap > svg')
  await map.click({ position: { x: 145, y: 80 } })
  await mapReady(page)
  await clickEmptyMap(page)
  await expect.poll(async () => (await camera(page)).x).toBeGreaterThan(initial.x)
  await expectEmptyMapCamera(page)
  const canvas = (await page.locator('canvas').boundingBox())!
  await page.mouse.move(canvas.x + 700, canvas.y + 500)
  await page.mouse.wheel(0, -100)
  await expect.poll(async () => (await camera(page)).zoom).toBeCloseTo(1, 4)
  await settle(page)
  const { x, y, zoom } = await camera(page)
  const target = rooms.find((room) => room.id === 'voice')!
  await page.mouse.dblclick(
    canvas.x + (target.spots.desk!.x - x) * zoom,
    canvas.y + (target.spots.desk!.y - y) * zoom,
  )
  await settle(page)
  await expectCamera(
    page,
    clampCamera(
      {
        x: target.spots.desk!.x - canvas.width / zoom / 2,
        y: target.spots.desk!.y - canvas.height / zoom / 2,
        zoom,
      },
      canvas.width,
      canvas.height,
      campus,
    ),
  )
  await expect(page).not.toHaveURL(/room=/)
})

test('world art and engine stay lazy until the first switch', async ({ page }) => {
  const requests: string[] = []
  page.on('request', (request) => requests.push(request.url()))
  await page.goto('/agents/excalibur?view=chat&worldDebug=1&worldClock=manual')
  await expect(page.getByRole('heading', { name: 'Excalibur', exact: true })).toBeVisible()
  expect(requests.some((url) => /assets\/World-.*\.js|\/world\/campus.webp/.test(url))).toBe(false)
  await page
    .getByRole('group', { name: 'Modo de vista' })
    .getByRole('button', { name: 'Mundo 2D', exact: true })
    .click()
  await worldReady(page)
  expect(requests.some((url) => /\/world\/campus.webp/.test(url))).toBe(true)
})

test('rich seed has populated activity, eight rooms, named helpers and locale-aware times', async ({
  page,
}) => {
  await page.goto('/agents/excalibur?view=world&worldDebug=1&worldClock=manual')
  await worldReady(page)
  await expect(page.locator('.world-room-access')).toHaveCount(0)
  await expect(page.locator('[data-world-agent]')).toHaveCount(4)
  await expect(page.locator('.world-activity .world-badge')).toContainText(/1[6-9]|2\d/)
  await expect(page.locator('.world-activity-entry').first()).toBeVisible()
  const time = await page.locator('.world-activity-entry small').first().textContent()
  expect(time).toMatch(/\d{2}:\d{2} ·/)
  expect(time).not.toMatch(/AM|PM/)
  await visitMapRoom(page, 'Proyecto Aurora')
  await expect(page).toHaveURL(/room=launch/)
  await worldReady(page)
  await expect(page.locator('.world-render-error')).toHaveCount(0)
  await expect(page.locator('.world-missions progress')).toHaveAttribute('value', '30')
  await page.locator('.world-back').click()
  await visitMapRoom(page, 'Observatorio')
  await expect(page).toHaveURL(/room=observatory/)
  await worldReady(page)
})

test('mobile starts with small pills, hides room chips and leaves the canvas draggable', async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.addInitScript(() => localStorage.setItem('world-activity', '0'))
  await page.goto('/agents/excalibur?view=world&worldDebug=1&worldClock=manual')
  await worldReady(page)
  await expect(page.locator('.world-missions .world-panel-title')).toHaveAttribute(
    'aria-expanded',
    'false',
  )
  await expect(page.locator('.world-activity .world-panel-title')).toHaveAttribute(
    'aria-expanded',
    'false',
  )
  expect((await page.locator('.world-missions').boundingBox())!.height).toBeLessThan(45)
  expect((await page.locator('.world-activity').boundingBox())!.height).toBeLessThan(45)
  await expect(page.locator('.world-room-access')).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Salas', exact: true })).toHaveCount(0)
  await expect(page.locator('.sidebar-rail')).toHaveCount(0)
  const canvas = (await page.locator('canvas').boundingBox())!
  await page.mouse.move(canvas.x + 210, canvas.y + 350)
  await page.mouse.down()
  await page.mouse.move(canvas.x + 90, canvas.y + 390, { steps: 8 })
  await page.mouse.up()
  await expect(page).not.toHaveURL(/room=/)
  await openMap(page)
  await page.getByRole('button', { name: 'Cerrar mapa', exact: true }).click()
  await expect(page.getByRole('dialog', { name: 'Mapa del campus' })).toBeHidden()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
})

test('accelerated scripted events remove and recreate helpers through the generic contract', async ({
  page,
}) => {
  await page.goto('/agents/excalibur?view=world&worldDebug=1&worldClock=manual&worldSpeed=16')
  await worldReady(page)
  const helper = page.locator('[data-world-agent="source-checker"]')
  await expect(helper).toHaveCount(1)
  const removal = timeline.find((entry) =>
    entry.event.agentChanges?.remove?.includes('source-checker'),
  )!
  const recreation = timeline.find(
    (entry) =>
      entry.offset > removal.offset &&
      entry.event.agentChanges?.upsert?.some((agent) => agent.id === 'source-checker'),
  )!
  await advance(page, (removal.offset - 1) / 16)
  await expect(helper).toHaveCount(1)
  await advance(page, 1 / 16)
  await expect(helper).toHaveCount(0)
  await expect
    .poll(() => page.evaluate(() => window.__excaliburWorld?.elapsed))
    .toBe(removal.offset)
  await advance(page, (recreation.offset - removal.offset - 1) / 16)
  await expect(helper).toHaveCount(0)
  await advance(page, 1 / 16)
  await expect(helper).toHaveCount(1)
  await page
    .getByRole('group', { name: 'Modo de vista' })
    .getByRole('button', { name: 'Chat', exact: true })
    .click()
  await expect(page).toHaveURL(/worldSpeed=16/)
  await expect
    .poll(() => page.evaluate(() => window.__excaliburWorld))
    .toEqual({ running: false, fps: 0, mode: 'chat', ready: false })
})

test('minimap thumbnail and viewport stay aligned during click navigation and rectangle dragging', async ({
  page,
}) => {
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.goto('/agents/excalibur?view=world&worldDebug=1&worldClock=manual')
  await worldReady(page)
  const map = page.locator('.world-minimap > svg'),
    viewport = page.locator('.world-map-viewport')
  await expect(page.locator('.world-map-thumbnail')).toHaveAttribute('href', '/world/campus.webp')
  const box = (await map.boundingBox())!
  expect(box.width / box.height).toBeCloseTo(1.5, 2)
  const before = await camera(page)
  await map.click({ position: { x: box.width - 4, y: box.height / 2 } })
  await mapReady(page)
  await clickEmptyMap(page)
  await expect.poll(async () => (await camera(page)).x).toBeGreaterThan(before.x)
  await expectEmptyMapCamera(page)
  const stroke = await viewport.evaluate((node) => ({
    x: Number(node.getAttribute('x')),
    y: Number(node.getAttribute('y')),
    width: Number(node.getAttribute('width')),
    height: Number(node.getAttribute('height')),
    worldX: Number(node.getAttribute('data-world-x')),
    worldWidth: Number(node.getAttribute('data-world-width')),
  }))
  const current = await camera(page)
  const canvas = (await page.locator('.world-canvas').boundingBox())!
  expect(stroke.worldX).toBeCloseTo(current.x, 3)
  expect(stroke.worldWidth).toBeCloseTo(canvas.width / current.zoom, 3)
  expect(stroke.x).toBeCloseTo((stroke.worldX * 220) / 2400 + 0.75, 3)
  expect(stroke.width).toBeCloseTo((stroke.worldWidth * 220) / 2400 - 1.5, 3)
  expect(stroke.x + stroke.width + 0.75).toBeLessThanOrEqual(220.01)
  const handle = (await viewport.boundingBox())!
  const previous = await camera(page)
  await page.mouse.move(handle.x + handle.width / 2, handle.y + handle.height / 2)
  await page.mouse.down()
  await page.mouse.move(handle.x + handle.width / 2 - 14, handle.y + handle.height / 2, {
    steps: 5,
  })
  await page.mouse.up()
  await expect.poll(async () => (await camera(page)).x).toBeLessThan(previous.x)
  const mapCard = (await page.locator('.world-minimap').boundingBox())!,
    bottom = (await page.locator('.world-bottom').boundingBox())!
  expect(mapCard.y + mapCard.height).toBeLessThan(bottom.y)
})

test('world settings enforce zoom limits, persist speed and zoom, and remain available in rooms', async ({
  page,
}) => {
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.goto('/agents/excalibur?view=world&worldDebug=1&worldClock=manual')
  await worldReady(page)
  const trigger = page.getByRole('button', { name: 'Ajustes del mundo', exact: true })
  const position = (await trigger.boundingBox())!
  await trigger.click()
  const minimapBox = (await page.locator('.world-minimap').boundingBox())!
  const activityBox = (await page.locator('.world-activity').boundingBox())!
  const popupBox = (await page.locator('.world-settings-popover').boundingBox())!
  expect(minimapBox.y).toBeGreaterThan(activityBox.y + activityBox.height)
  expect(minimapBox.x + minimapBox.width).toBeLessThan(popupBox.x)

  const increase = page.getByRole('button', { name: 'Aumentar zoom' }),
    decrease = page.getByRole('button', { name: 'Reducir zoom' })
  for (let index = 0; index < 3; index++) await increase.click()
  await expect(increase).toBeDisabled()
  await expect(page.locator('.world-pill-zoom')).toHaveText('150 %')
  for (let index = 0; index < 3; index++) await decrease.click()
  await expect(decrease).toBeDisabled()
  await expect(page.locator('.world-pill-zoom')).toHaveText('75 %')
  await page.getByRole('button', { name: 'Aumentar velocidad', exact: true }).click()
  await increase.click()
  expect(await page.evaluate(() => localStorage.getItem('world-speed'))).toBe('2')
  expect(await page.evaluate(() => localStorage.getItem('world-zoom'))).toBe('1')
  await page.keyboard.press('Escape')
  await expect(page.locator('.world-settings-popover')).toHaveCount(0)
  await page.reload()
  await worldReady(page)
  await trigger.click()
  await expect(page.locator('.world-pill-zoom')).toHaveText('100 %')
  await expect(page.locator('.world-settings-speed output')).toHaveText('2×')
  await trigger.click()
  await visitMapRoom(page, 'Modelos de voz')
  await worldReady(page)
  const roomPosition = (await trigger.boundingBox())!
  expect(roomPosition.x).toBe(position.x)
  expect(roomPosition.y).toBe(position.y)
  await trigger.click()
  await increase.click()
  await expect(page.locator('.world-pill-zoom')).toHaveText('125 %')
  await page.keyboard.press('Escape')
  await expect(page).toHaveURL(/room=voice/)
})

test('walking diagnostics report running and measured FPS, then stop in Chat', async ({ page }) => {
  await page.goto('/agents/excalibur?view=world&worldDebug=1&worldClock=manual')
  await worldReady(page)
  await advance(page, timeline[0]!.offset)
  const agent = page.locator('[data-world-agent="excalibur"]')
  const initial = await agent.getAttribute('style')
  await advance(page, 160)
  await expect.poll(async () => await agent.getAttribute('style')).not.toBe(initial)
  await expect.poll(() => page.evaluate(() => window.__excaliburWorld?.running)).toBe(true)
  await expect.poll(() => page.evaluate(() => window.__excaliburWorld?.fps ?? 0)).toBeGreaterThan(0)
  expect(await page.evaluate(() => window.__excaliburWorld!.fps)).toBeLessThanOrEqual(30)
  await page
    .getByRole('group', { name: 'Modo de vista' })
    .getByRole('button', { name: 'Chat', exact: true })
    .click()
  await expect
    .poll(() => page.evaluate(() => window.__excaliburWorld))
    .toEqual({ running: false, fps: 0, mode: 'chat', ready: false })
})

test.describe('touch world controls', () => {
  test.use({ hasTouch: true, isMobile: true })
  test('two-finger pinch snaps zoom while one finger only pans at 390 px', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 })
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.goto('/agents/excalibur?view=world&worldDebug=1&worldClock=manual')
    await worldReady(page)
    await expect(page.locator('.world-canvas')).toHaveCSS('touch-action', 'none')
    const session = await page.context().newCDPSession(page)
    await session.send('Input.dispatchTouchEvent', {
      type: 'touchStart',
      touchPoints: [
        { id: 1, x: 145, y: 520 },
        { id: 2, x: 255, y: 520 },
      ],
    })
    await session.send('Input.dispatchTouchEvent', {
      type: 'touchMove',
      touchPoints: [
        { id: 1, x: 70, y: 520 },
        { id: 2, x: 330, y: 520 },
      ],
    })
    await session.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] })
    await expect.poll(() => page.evaluate(() => localStorage.getItem('world-zoom'))).toBe('1.5')
    await session.send('Input.dispatchTouchEvent', {
      type: 'touchStart',
      touchPoints: [{ id: 1, x: 280, y: 520 }],
    })
    await session.send('Input.dispatchTouchEvent', {
      type: 'touchMove',
      touchPoints: [{ id: 1, x: 250, y: 560 }],
    })
    await session.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] })
    expect(await page.evaluate(() => localStorage.getItem('world-zoom'))).toBe('1.5')
    await page.getByRole('button', { name: 'Ajustes del mundo', exact: true }).tap()
    await expect(page.locator('.world-pill-zoom')).toHaveText('150 %')
    await expect(page.getByRole('button', { name: 'Aumentar zoom' })).toBeDisabled()
  })
})

test('campus signs, agent names and status icons retain screen size at maximum zoom', async ({
  page,
}) => {
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.goto('/agents/excalibur?view=world&worldDebug=1&worldClock=manual&worldSpeed=0.25')
  await worldReady(page)
  await page.getByRole('button', { name: 'Ajustes del mundo', exact: true }).click()
  const increase = page.getByRole('button', { name: 'Aumentar zoom' })
  await increase.click()
  await expect(page.locator('.world-pill-zoom')).toHaveText('100 %')
  const sign = page.locator('.world-room-sign').filter({ hasText: 'Modelos de voz' })
  const name = page.locator('[data-world-agent="excalibur"] .world-agent-name')
  const initialSign = (await sign.boundingBox())!,
    initialName = (await name.boundingBox())!,
    initialIcon = (await name.locator('svg').boundingBox())!
  const tooltip = await name.getAttribute('title')
  for (let step = 0; step < 2; step++) await increase.click()
  await expect(increase).toBeDisabled()
  await expect(page.locator('.world-pill-zoom')).toHaveText('150 %')
  const maximumSign = (await sign.boundingBox())!,
    maximumName = (await name.boundingBox())!,
    maximumIcon = (await name.locator('svg').boundingBox())!
  expect(Math.abs(maximumSign.height - initialSign.height)).toBeLessThanOrEqual(2)
  expect(Math.abs(maximumSign.width - initialSign.width)).toBeLessThanOrEqual(2)
  expect(Math.abs(maximumName.height - initialName.height)).toBeLessThanOrEqual(2)
  expect(Math.abs(maximumName.width - initialName.width)).toBeLessThanOrEqual(2)
  expect(Math.abs(maximumIcon.height - initialIcon.height)).toBeLessThanOrEqual(2)
  await expect(name).toHaveAttribute('title', tooltip!)
  await expect(page.locator('.world-canvas canvas')).toHaveCSS('image-rendering', 'pixelated')
  const minimap = (await page.locator('.world-minimap').boundingBox())!,
    popup = (await page.locator('.world-settings-popover').boundingBox())!
  expect(minimap.x + minimap.width).toBeLessThan(popup.x)
})

test('room agent names and icons retain screen size when interior zoom changes', async ({
  page,
}) => {
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.goto(
    '/agents/excalibur?view=world&room=voice&worldDebug=1&worldClock=manual&worldSpeed=0.25',
  )
  await worldReady(page)
  await page.getByRole('button', { name: 'Ajustes del mundo', exact: true }).click()
  const increase = page.getByRole('button', { name: 'Aumentar zoom' })
  await increase.click()
  await expect(page.locator('.world-pill-zoom')).toHaveText('100 %')
  const name = page.locator('[data-world-agent="excalibur"] .world-agent-name')
  const initial = (await name.boundingBox())!,
    initialIcon = (await name.locator('svg').boundingBox())!
  for (let step = 0; step < 2; step++) await increase.click()
  await expect(page.locator('.world-pill-zoom')).toHaveText('150 %')
  const maximum = (await name.boundingBox())!,
    maximumIcon = (await name.locator('svg').boundingBox())!
  expect(Math.abs(maximum.height - initial.height)).toBeLessThanOrEqual(2)
  expect(Math.abs(maximum.width - initial.width)).toBeLessThanOrEqual(2)
  expect(Math.abs(maximumIcon.height - initialIcon.height)).toBeLessThanOrEqual(2)
})

test('full map opens from the minimap, selects polygons or list items, and navigates empty space', async ({
  page,
}) => {
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.goto('/agents/excalibur?view=world&worldDebug=1&worldClock=manual&worldSpeed=0.25')
  await worldReady(page)
  await expect(page.locator('.world-room-access')).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Salas', exact: true })).toHaveCount(0)
  await page.locator('.world-minimap > svg').click({ position: { x: 100, y: 80 } })
  const modal = page.getByRole('dialog', { name: 'Mapa del campus' })
  await mapReady(page)
  await expect(modal).toHaveAttribute('aria-modal', 'true')
  await expect(page.locator('.world-map-room-list > button')).toHaveCount(8)
  await expect(page.locator('.world-full-map-label .pixel-avatar').first()).toBeVisible()
  const close = page.getByRole('button', { name: 'Cerrar mapa', exact: true })
  await expect(close).toBeFocused()
  await page.keyboard.press('Shift+Tab')
  await expect(page.locator('.world-map-room-list > button').last()).toBeFocused()
  await page.keyboard.press('Tab')
  await expect(close).toBeFocused()
  const image = (await page.locator('.world-full-map-image').boundingBox())!
  const room = rooms.find((room) => room.id === 'library')!,
    center = {
      x: room.polygon.reduce((sum, p) => sum + p.x, 0) / room.polygon.length,
      y: room.polygon.reduce((sum, p) => sum + p.y, 0) / room.polygon.length,
    }
  const point = {
    x: center.x * 0.35 + room.polygon[0]!.x * 0.65,
    y: center.y * 0.35 + room.polygon[0]!.y * 0.65,
  }
  await page.mouse.click(
    image.x + (point.x / campus.width) * image.width,
    image.y + (point.y / campus.height) * image.height,
  )
  await expect(page).toHaveURL(/room=library/)
  await expect(modal).toBeHidden()
  await expect(page.locator('.world-room-access')).toHaveCount(0)
  await visitMapRoom(page, 'Código')
  await expect(page).toHaveURL(/room=code/)
  await openMap(page)
  await clickEmptyMap(page)
  await expect(page).not.toHaveURL(/room=/)
  await worldReady(page)
  await expectEmptyMapCamera(page)
  await openMap(page)
  await page.keyboard.press('Escape')
  await expect(modal).toBeHidden()
  await expect(page.getByRole('button', { name: 'Abrir mapa completo', exact: true })).toBeFocused()
  await openMap(page)
  await page.mouse.click(2, 2)
  await expect(modal).toBeHidden()
})

test('settings form a rounded vertical column centered above the gear with a cycling speed', async ({
  page,
}) => {
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.goto('/agents/excalibur?view=world&worldDebug=1&worldClock=manual')
  await worldReady(page)
  const gear = page.getByRole('button', { name: 'Ajustes del mundo', exact: true })
  await gear.click()
  const pill = page.getByRole('toolbar', { name: 'Ajustes del mundo' }),
    box = (await pill.boundingBox())!,
    trigger = (await gear.boundingBox())!
  expect(Math.abs(box.x + box.width / 2 - trigger.x - trigger.width / 2)).toBeLessThan(1)
  expect(Math.abs(box.width - trigger.width)).toBeLessThanOrEqual(2)
  expect(box.y + box.height).toBeLessThan(trigger.y)
  expect(
    parseFloat(await pill.evaluate((node) => getComputedStyle(node).borderTopLeftRadius)),
  ).toBeGreaterThanOrEqual(box.width / 2)
  const minimap = (await page.locator('.world-minimap').boundingBox())!
  expect(minimap.x + minimap.width).toBeLessThan(box.x)
  for (const value of ['2×', '4×', '0,5×', '1×']) {
    await page.getByRole('button', { name: 'Aumentar velocidad', exact: true }).click()
    await expect(page.locator('.world-settings-speed output')).toHaveText(value)
  }
  await page.getByRole('button', { name: 'Aumentar velocidad', exact: true }).focus()
  await expect(page.getByRole('tooltip', { name: 'Aumentar velocidad', exact: true })).toBeVisible()
  await page.keyboard.press('ArrowDown')
  await expect(page.getByRole('button', { name: 'Aumentar zoom', exact: true })).toBeFocused()
  await page.locator('.world-missions .world-panel-title').click()
  await expect(pill).toHaveCount(0)
})

test('mobile full map fills 390 by 844 and keeps map navigation and vertical settings usable', async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.goto('/agents/excalibur?view=world&worldDebug=1&worldClock=manual')
  await worldReady(page)
  await expect(page.locator('.world-room-access')).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Salas', exact: true })).toHaveCount(0)
  await openMap(page)
  const modal = page.getByRole('dialog', { name: 'Mapa del campus' }),
    box = (await modal.boundingBox())!
  expect(box).toEqual({ x: 0, y: 0, width: 390, height: 844 })
  await page.locator('.world-map-room-list > button').filter({ hasText: 'Finanzas' }).click()
  await expect(page).toHaveURL(/room=finance/)
  await expect(modal).toBeHidden()
  const gear = page.getByRole('button', { name: 'Ajustes del mundo', exact: true })
  await gear.click()
  const column = (await page.getByRole('toolbar', { name: 'Ajustes del mundo' }).boundingBox())!,
    trigger = (await gear.boundingBox())!
  expect(column.y + column.height).toBeLessThan(trigger.y)
  expect(Math.abs(column.x + column.width / 2 - trigger.x - trigger.width / 2)).toBeLessThan(1)
  await page.getByRole('button', { name: 'Aumentar velocidad', exact: true }).click()
  await expect(page.locator('.world-settings-speed output')).toHaveText('2×')
  await page.keyboard.press('Escape')
  await expect(page).toHaveURL(/room=finance/)
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
})

test('compact chat actions fit mobile and narrow columns with keyboard-accessible overflow', async ({
  page,
}) => {
  for (const width of [1536, 390]) {
    await page.setViewportSize({ width, height: 844 })
    await page.goto('/agents/excalibur?view=chat')
    if (width === 1536) {
      await page.getByRole('button', { name: 'Alternar panel', exact: true }).click()
      const handle = page.getByRole('separator', { name: 'Cambiar ancho del panel', exact: true })
      const box = (await handle.boundingBox())!
      await page.mouse.move(box.x + box.width / 2, box.y + 100)
      await page.mouse.down()
      await page.mouse.move(box.x - 200, box.y + 100, { steps: 5 })
      await page.mouse.up()
      expect((await page.locator('.chat-column').boundingBox())!.width).toBeLessThan(480)
    }
    const header = page.locator('.chat-header')
    const box = (await header.boundingBox())!
    for (const button of await header.getByRole('button').all()) {
      const bounds = (await button.boundingBox())!
      expect(bounds.x).toBeGreaterThanOrEqual(box.x)
      expect(bounds.x + bounds.width).toBeLessThanOrEqual(Math.min(width, box.x + box.width))
      expect(bounds.y + bounds.height).toBeLessThanOrEqual(box.y + box.height)
    }
    await expect(header.getByRole('button', { name: 'Mundo 2D', exact: true })).toBeVisible()
    await expect(header.getByRole('button', { name: 'Alternar panel', exact: true })).toBeVisible()
    await expect(header.getByRole('button', { name: 'Llamar', exact: true })).toBeHidden()
    const overflow = header.getByRole('button', { name: 'Más opciones', exact: true })
    await overflow.focus()
    await page.keyboard.press('ArrowDown')
    await expect(page.getByRole('menuitem', { name: 'Llamar', exact: true })).toBeFocused()
    await page.keyboard.press('ArrowDown')
    await page.keyboard.press('Enter')
    await expect(page.getByRole('dialog')).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(page.getByRole('dialog')).toHaveCount(0)
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  }
})

test('world chrome uses theme accents and disables decorative motion when requested', async ({
  page,
}) => {
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.goto('/agents/excalibur?view=world&worldDebug=1&worldClock=manual')
  await worldReady(page)
  const live = page.locator('.world-live-dot')
  await expect(live).toHaveCSS('animation-name', 'none')
  await expect(page.locator('.world-map-active-agent')).toHaveCSS('animation-name', 'none')
  for (const dark of [false, true]) {
    await page.evaluate(
      (enabled) => document.documentElement.classList.toggle('dark', enabled),
      dark,
    )
    const accent = await live.evaluate((node) => getComputedStyle(node).backgroundColor)
    await expect(page.locator('.world-map-viewport')).toHaveCSS('stroke', accent)
    await expect(page.locator('.world-enter button[type="submit"]')).toHaveCSS(
      'background-color',
      accent,
    )
    await expect(page.locator('.world-enter')).toHaveCSS(
      'background-color',
      await page
        .locator('.world-map-modal')
        .evaluate((node) => getComputedStyle(node).backgroundColor),
    )
  }
  await page.evaluate(() => document.documentElement.classList.remove('dark'))
  await page.emulateMedia({ reducedMotion: 'no-preference' })
  await expect(live).toHaveCSS('animation-name', 'rd-world-live')
  await expect(page.locator('.world-map-active-agent')).toHaveCSS('animation-name', 'none')
})
