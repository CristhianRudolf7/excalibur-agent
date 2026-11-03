import { test, expect } from './fixtures'
import { mockChat } from './chat-fixture'

for (const reducedMotion of ['no-preference', 'reduce'] as const) {
  test(`initial history uses message skeletons while delayed (${reducedMotion})`, async ({
    page,
  }) => {
    await page.emulateMedia({ reducedMotion })
    await mockChat(page, [{ text: 'The conversation is ready' }])
    let release!: () => void
    const pending = new Promise<void>((resolve) => {
      release = resolve
    })
    await page.route('**/rpc', async (route) => {
      if (route.request().postDataJSON().method !== 'conversation.history') return route.fallback()
      await pending
      return route.fallback()
    })
    await page.goto('/agents/excalibur?view=chat')
    const skeleton = page.getByRole('status', { name: 'Cargando mensajes' })
    await expect(skeleton).toBeVisible()
    await expect(skeleton.locator('.sk-message-0')).toHaveCount(2)
    await expect(skeleton.locator('.sk-message-1')).toHaveCount(2)
    await expect(page.locator('.chat-body')).not.toContainText(
      /Sin conexión|Conectando|Reconectando/,
    )
    await expect(page.locator('.chat-body [role="alert"]')).toHaveCount(0)
    expect(
      await skeleton
        .locator('.skeleton')
        .first()
        .evaluate((node) => getComputedStyle(node, '::after').animationName),
    ).toBe(reducedMotion === 'reduce' ? 'none' : 'shell-shimmer')
    release()
    await expect(page.getByText('The conversation is ready', { exact: true })).toBeVisible()
    await expect(skeleton).toHaveCount(0)
  })
}

test('keeps conversation and panel data through an outage, retries offline sends and merges catch-up once', async ({
  page,
}) => {
  await page.emulateMedia({ reducedMotion: 'reduce' })
  const chat = await mockChat(page, [
    {
      text: 'An answer already on screen',
      changes: [
        {
          path: 'src/preserved.ts',
          status: 'added',
          added: 1,
          removed: 0,
          content: 'kept',
          hunks: [],
        },
      ],
    },
  ])
  let offline = false
  let holdHistory = false
  let release!: () => void
  const pending = new Promise<void>((resolve) => {
    release = resolve
  })
  const clientIds: string[] = []
  await page.route('**/rpc', async (route) => {
    const request = route.request().postDataJSON()
    if (!['conversation.history', 'conversation.send', 'activity.list'].includes(request.method))
      return route.fallback()
    if (request.method === 'conversation.send') clientIds.push(request.params.client_msg_id)
    if (offline) return route.abort('connectionrefused')
    if (request.method === 'conversation.history' && holdHistory) await pending
    return route.fallback()
  })
  await page.goto('/agents/excalibur?view=chat')
  const answer = page.getByText('An answer already on screen', { exact: true })
  const indicator = page.locator('.chat-body .chat-connection[role="status"]')
  await expect(answer).toBeVisible()
  await expect.poll(chat.connected).toBe(true)
  await expect(indicator).toHaveCount(0)
  await page.getByRole('button', { name: 'Ver 1 archivo modificado en el panel' }).click()
  const change = page.locator('.panel-changes').getByText('preserved.ts', { exact: true })
  await expect(change).toBeVisible()
  offline = true
  chat.pause()
  await expect(indicator).toHaveText('Reconectando…')
  await expect(answer).toBeVisible()
  await expect(change).toBeVisible()
  await expect(indicator.locator('.chat-connection-dot')).toHaveCSS('animation-name', 'none')
  const composer = page.getByRole('textbox', { name: 'Escribe a Excalibur…' })
  await expect(composer).toBeEnabled()
  await composer.fill('Send this when we reconnect')
  await composer.press('Enter')
  await expect(page.locator('.message-user')).toContainText('Send this when we reconnect')
  await expect.poll(() => clientIds.length).toBe(1)
  // This event is lost while the socket is down; the persisted history must recover it.
  const missed = {
    id: 'outage-message',
    conversation_id: 'cnv_web_excalibur',
    role: 'assistant',
    text: 'Arrived during the outage',
  }
  chat.messages.push(missed)
  chat.emit('message.created', { ...missed, message_id: missed.id })
  await expect(answer).toBeVisible()
  holdHistory = true
  offline = false
  chat.resume()
  await expect.poll(() => clientIds.length).toBe(2)
  await expect(indicator).toBeVisible()
  await expect(answer).toBeVisible()
  await expect(change).toBeVisible()
  release()
  const caughtUp = page.getByText(missed.text, { exact: true })
  await expect(caughtUp).toBeVisible()
  await expect(indicator).toHaveCount(0)
  expect(clientIds[0]).toBe(clientIds[1])
  // A late replay and another resubscription must not duplicate or move messages.
  chat.emit('message.created', { ...missed, message_id: missed.id })
  chat.disconnect()
  await expect(indicator).toBeVisible()
  await expect(caughtUp).toBeVisible()
  await expect(answer).toBeVisible()
  await expect(indicator).toHaveCount(0)
  await expect(caughtUp).toHaveCount(1)
  await expect(page.locator('.message-user')).toHaveCount(1)
  await expect(page.locator('.message-agent .markdown')).toHaveText([
    'An answer already on screen',
    'Arrived during the outage',
  ])
})

test('only shows a recoverable history error after retries and keeps known messages during a failed refetch', async ({
  page,
}) => {
  const chat = await mockChat(page, [{ text: 'Recovered history' }])
  let failing = true
  let attempts = 0
  await page.route('**/rpc', async (route) => {
    const request = route.request().postDataJSON()
    if (request.method !== 'conversation.history') return route.fallback()
    attempts++
    if (failing) return route.abort('connectionrefused')
    return route.fallback()
  })
  await page.goto('/agents/excalibur?view=chat')
  await expect(page.locator('.sk-history')).toBeVisible()
  await expect(page.locator('.chat-body')).not.toContainText(/Sin conexión|Reconectando/)
  const error = page.locator('.chat-body [role="alert"]')
  await expect(error).toContainText('No se pudo cargar el historial.')
  expect(attempts).toBe(3)
  failing = false
  await error.getByRole('button', { name: 'Reintentar' }).click()
  await expect(page.getByText('Recovered history', { exact: true })).toBeVisible()
  failing = true
  chat.disconnect()
  await expect(error).toContainText('No se pudo cargar el historial.')
  await expect(page.getByText('Recovered history', { exact: true })).toBeVisible()
  failing = false
  await error.getByRole('button', { name: 'Reintentar' }).click()
  await expect(error).toHaveCount(0)
  await expect(page.getByText('Recovered history', { exact: true })).toHaveCount(1)
})
