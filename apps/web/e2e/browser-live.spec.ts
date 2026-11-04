import { rm } from 'node:fs/promises'
import { test, expect } from './fixtures'
import { mockEvents } from './event-fixture'
import { mockChat } from './chat-fixture'
import type { BrowserLiveState, BrowserSessionEvent } from '../src/features/browserlive/types'

// A generated 16x8 JPEG of a solid local test page; it contains no browsing data.
const jpeg = Buffer.from(
  '/9j/4AAQSkZJRgABAQAAAQABAAD/2wBDAAgGBgcGBQgHBwcJCQgKDBQNDAsLDBkSEw8UHRofHh0aHBwgJC4nICIsIxwcKDcpLDAxNDQ0Hyc5PTgyPC4zNDL/2wBDAQkJCQwLDBgNDRgyIRwhMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjL/wAARCAAIABADASIAAhEBAxEB/8QAHwAAAQUBAQEBAQEAAAAAAAAAAAECAwQFBgcICQoL/8QAtRAAAgEDAwIEAwUFBAQAAAF9AQIDAAQRBRIhMUEGE1FhByJxFDKBkaEII0KxwRVS0fAkM2JyggkKFhcYGRolJicoKSo0NTY3ODk6Q0RFRkdISUpTVFVWV1hZWmNkZWZnaGlqc3R1dnd4eXqDhIWGh4iJipKTlJWWl5iZmqKjpKWmp6ipqrKztLW2t7i5usLDxMXGx8jJytLT1NXW19jZ2uHi4+Tl5ufo6erx8vP09fb3+Pn6/8QAHwEAAwEBAQEBAQEBAQAAAAAAAAECAwQFBgcICQoL/8QAtREAAgECBAQDBAcFBAQAAQJ3AAECAxEEBSExBhJBUQdhcRMiMoEIFEKRobHBCSMzUvAVYnLRChYkNOEl8RcYGRomJygpKjU2Nzg5OkNERUZHSElKU1RVVldYWVpjZGVmZ2hpanN0dXZ3eHl6goOEhYaHiImKkpOUlZaXmJmaoqOkpaanqKmqsrO0tba3uLm6wsPExcbHyMnK0tPU1dbX2Nna4uPk5ebn6Onq8vP09fb3+Pn6/9oADAMBAAIRAxEAPwD0iiiigD//2Q==',
  'base64',
)
function frame(seq = 1) {
  const header = Buffer.from(
    JSON.stringify({ seq, tab_id: 'tab', width: 800, height: 400, at: Date.now() }),
  )
  const length = Buffer.alloc(4)
  length.writeUInt32BE(header.length)
  return Buffer.concat([length, header, jpeg])
}
const session: BrowserSessionEvent = {
  conversation_id: 'cnv_web_excalibur',
  session_id: 'local-session',
  run_id: 'run',
  state: 'active',
  url: `https://example.test/${'long-path/'.repeat(40)}`,
  title: 'Local browser page',
}
for (const width of [1280, 390]) {
  test(`watch-only browser lifetime and responsive panel at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    await page.addInitScript(() => localStorage.setItem('excalibur-ui-theme', 'light'))
    const bus = await mockEvents(page)
    let state: BrowserLiveState = {}
    await page.route('**/rpc', async (route) => {
      const request = route.request().postDataJSON()
      if (request.method !== 'browser.live_state') return route.fallback()
      expect(request.params.conversation_id).toBe(session.conversation_id)
      await route.fulfill({ json: { jsonrpc: '2.0', id: request.id, result: state } })
    })
    const messages: (string | Buffer)[] = []
    let connections = 0,
      closed = 0
    await page.routeWebSocket('**/ws/browser**', (socket) => {
      connections++
      expect(new URL(socket.url()).searchParams.get('session_id')).toBe(session.session_id)
      socket.onMessage((data) => messages.push(data))
      socket.onClose(() => closed++)
      socket.send(frame())
    })
    await page.goto('/agents/excalibur?view=chat')
    await bus.ready('browser.session')
    await expect(page.locator('.right-panel')).toHaveCount(0)
    expect(connections).toBe(0)
    state = { session }
    bus.emit('browser.session', session)
    await expect(page.locator('.browser-live')).toBeVisible()
    await expect(page.getByText('Local browser page')).toBeVisible()
    await expect(page.locator('.browser-live-url')).toHaveAttribute('title', session.url!)
    await expect(page.locator('.browser-live-skeleton')).toHaveCount(0)
    const activity = {
      conversation_id: session.conversation_id,
      session_id: session.session_id,
      run_id: 'run',
      seq: 1,
      phase: 'start' as const,
      kind: 'click',
      target: 'Continue',
      box: { x: 100, y: 50, width: 200, height: 100 },
    }
    state.activity = activity
    bus.emit('browser.activity', activity)
    await expect(page.locator('.browser-live-action')).toHaveText("Clic en 'Continue'")
    await expect
      .poll(async () => {
        const highlight = await page.getByTestId('browser-highlight').boundingBox()
        const stage = await page.locator('.browser-live-stage').boundingBox()
        return Math.abs(highlight!.width - stage!.width / 4)
      })
      .toBeLessThan(0.1)
    await expect(page.getByTestId('browser-highlight')).toHaveAttribute('style', /left: 12.5%/)
    const stage = page.locator('.browser-live-stage')
    expect(
      await stage.locator('button, input, textarea, [tabindex], [contenteditable]').count(),
    ).toBe(0)
    const rect = await stage.boundingBox()
    await page.mouse.click(rect!.x + rect!.width / 2, rect!.y + rect!.height / 2)
    expect(messages).toEqual([])
    await expect(page.locator('.message-user')).toHaveCount(0)
    for (const dark of [false, true]) {
      await page.evaluate((value) => document.documentElement.classList.toggle('dark', value), dark)
      expect(
        await page.locator('.browser-live').evaluate((node) => {
          const probe = document.createElement('div')
          probe.style.backgroundColor = 'var(--surface)'
          node.append(probe)
          const match =
            getComputedStyle(node).backgroundColor === getComputedStyle(probe).backgroundColor
          probe.remove()
          const styles = [...document.styleSheets]
            .flatMap((sheet) => {
              try {
                return [...sheet.cssRules]
                  .map((rule) => rule.cssText)
                  .filter((text) => text.includes('.browser-live'))
              } catch {
                return []
              }
            })
            .join('\n')
          return match && !/#[\da-f]{3,8}\b|rgba?\(\s*\d/i.test(styles)
        }),
      ).toBe(true)
    }
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await expect(page.locator('.browser-live-highlight')).toHaveCSS('transition-duration', '0s')
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    const screenshot = `/tmp/excalibur-browser-live-${process.pid}-${width}.png`
    await page.screenshot({ path: screenshot, animations: 'disabled' })
    await rm(screenshot)
    await page
      .locator('.panel-navigation')
      .getByRole('button', { name: 'Cerrar', exact: true })
      .click()
    await expect(page.locator('.right-panel')).toHaveCount(0)
    await expect.poll(() => closed).toBe(1)
    const reopen = page.getByRole('button', { name: 'Mostrar navegador en vivo' })
    await expect(reopen).toBeVisible()
    bus.emit('browser.session', session)
    await expect(page.locator('.right-panel')).toHaveCount(0)
    await reopen.click()
    await expect(page.locator('.browser-live')).toBeVisible()
    await expect.poll(() => connections).toBe(2)
    await page.getByRole('button', { name: 'Volver al inicio del panel' }).click()
    const card = page.locator('.panel-launcher').filter({ hasText: 'Navegador' })
    await expect(card).toBeVisible()
    await card.click()
    await expect(page.locator('.browser-live')).toBeVisible()
    state = { session: { ...session, state: 'ended' } }
    bus.emit('browser.session', state.session)
    await expect(page.getByText('La sesión del navegador terminó')).toBeVisible()
    await expect(page.locator('.browser-live-ended canvas')).toHaveCSS('opacity', '0.45')
    expect(messages).toEqual([])
    await expect.poll(() => closed).toBe(connections)
    await expect(reopen).toHaveCount(0)
  })
}

test('fresh and stale replay after reload restore the reopen control without auto-opening', async ({
  page,
}) => {
  await mockEvents(page)
  let stale = true
  await page.routeWebSocket('**/ws', (socket) => {
    socket.onMessage((data) => {
      const request = JSON.parse(String(data))
      const events = request.params.types.includes('browser.session')
        ? [
            {
              id: 'replay',
              seq: 1,
              at: new Date(Date.now() - (stale ? 60_000 : 0)).toISOString(),
              type: 'browser.session',
              payload: session,
            },
          ]
        : []
      socket.send(
        JSON.stringify({ jsonrpc: '2.0', id: request.id, result: { events, cursor: '' } }),
      )
    })
  })
  await page.route('**/rpc', async (route) => {
    const request = route.request().postDataJSON()
    if (request.method !== 'browser.live_state') return route.fallback()
    await route.fulfill({ json: { jsonrpc: '2.0', id: request.id, result: { session } } })
  })
  let connections = 0
  await page.routeWebSocket('**/ws/browser**', () => {
    connections++
  })
  await page.goto('/agents/excalibur?view=chat')
  await expect(page.getByRole('button', { name: 'Mostrar navegador en vivo' })).toBeVisible()
  await expect(page.locator('.right-panel')).toHaveCount(0)
  stale = false
  await page.reload()
  await expect(page.getByRole('button', { name: 'Mostrar navegador en vivo' })).toBeVisible()
  await expect(page.locator('.right-panel')).toHaveCount(0)
  expect(connections).toBe(0)
})

test('credential requests close an overlay browser panel for this conversation', async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 })
  const bus = await mockEvents(page)
  let active = false
  await page.route('**/rpc', async (route) => {
    const request = route.request().postDataJSON()
    if (request.method !== 'browser.live_state') return route.fallback()
    await route.fulfill({
      json: { jsonrpc: '2.0', id: request.id, result: active ? { session } : {} },
    })
  })
  await page.routeWebSocket('**/ws/browser**', (socket) => socket.send(frame()))
  await page.goto('/agents/excalibur?view=chat')
  await bus.ready('browser.session')
  active = true
  bus.emit('browser.session', session)
  await expect(page.locator('.browser-live')).toBeVisible()
  const request = {
    request_id: 'local-request',
    run_id: 'run',
    site: 'https://example.test',
    purpose: 'Local sign in',
    fields: ['password'],
    remember_default: false,
    created_at: new Date().toISOString(),
  }
  bus.emit('credentials.requested', { ...request, conversation_id: 'other' })
  await expect(page.locator('.browser-live')).toBeVisible()
  bus.emit('credentials.requested', { ...request, conversation_id: session.conversation_id })
  await expect(page.locator('.right-panel')).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Mostrar navegador en vivo' })).toBeVisible()
  await expect(page.getByLabel('Contraseña', { exact: true })).toBeVisible()
})

for (const reducedMotion of ['no-preference', 'reduce'] as const) {
  for (const following of [true, false]) {
    test(`panel reflow preserves chat scroll intent: following=${following}, motion=${reducedMotion}`, async ({
      page,
    }) => {
      await page.setViewportSize({ width: 1280, height: 900 })
      await page.emulateMedia({ reducedMotion })
      const text = 'A long answer wraps across several lines when the browser panel opens. '.repeat(
        9,
      )
      await mockChat(
        page,
        Array.from({ length: 14 }, (_, index) => ({ text: `${index + 1}. ${text}` })),
      )
      const bus = await mockEvents(page)
      let active = false
      await page.route('**/rpc', async (route) => {
        const request = route.request().postDataJSON()
        if (request.method !== 'browser.live_state') return route.fallback()
        await route.fulfill({
          json: { jsonrpc: '2.0', id: request.id, result: active ? { session } : {} },
        })
      })
      await page.routeWebSocket('**/ws/browser**', () => {})
      await page.goto('/agents/excalibur?view=chat')
      await bus.ready('browser.session')
      const messages = page.locator('.messages')
      await expect(page.locator('.message').first()).toBeVisible()
      // Virtual rows keep measuring while this runs, so scroll until it holds.
      await expect
        .poll(() =>
          messages.evaluate((node) => {
            node.scrollTop = node.scrollHeight
            return node.scrollHeight - node.scrollTop - node.clientHeight
          }),
        )
        .toBe(0)
      await expect(page.locator('.message[data-index="13"]')).toBeVisible()
      if (!following) {
        await messages.hover()
        await page.mouse.wheel(0, -100_000)
        await expect(messages).toHaveJSProperty('scrollTop', 0)
      }
      const beforeWidth = await messages.evaluate((node) => node.clientWidth)
      active = true
      bus.emit('browser.session', session)
      await expect(page.locator('.browser-live')).toBeVisible()
      await expect
        .poll(() => messages.evaluate((node) => node.clientWidth))
        .toBeLessThan(beforeWidth)
      if (following) {
        await expect
          .poll(async () => {
            const last = await page.locator('.message[data-index="13"]').boundingBox()
            const composer = await page.locator('.composer-area').boundingBox()
            const viewport = await messages.boundingBox()
            return (
              !!last &&
              !!composer &&
              !!viewport &&
              last.y + last.height <= composer.y &&
              last.y + last.height > viewport.y
            )
          })
          .toBe(true)
      } else {
        await expect(page.locator('.message[data-index="0"]')).toBeVisible()
        await expect(messages).toHaveJSProperty('scrollTop', 0)
        await expect(page.locator('.message[data-index="13"]')).toHaveCount(0)
      }
    })
  }
}
