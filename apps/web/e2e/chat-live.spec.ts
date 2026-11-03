import { test, expect } from './fixtures'
import { mockChat } from './chat-fixture'

test('one Excalibur in the sidebar and rail, with no create-agent control', async ({ page }) => {
  await page.goto('/agents/excalibur?view=chat')
  await expect(page.locator('.agent-nav button')).toHaveCount(1)
  await expect(page.locator('.agent-nav')).toContainText('Excalibur')
  await expect(page.getByRole('button', { name: 'Crear agente' })).toHaveCount(0)
  await page.keyboard.press('Control+b')
  await expect(page.locator('.sidebar-rail-agents button')).toHaveCount(1)
  await expect(page.locator('.sidebar-rail-agents button')).toHaveAttribute(
    'aria-label',
    'Excalibur',
  )
})

test('web and document sources open in a modal with focus containment and restoration', async ({
  page,
}) => {
  await mockChat(page, [
    {
      text: 'Verified answer [1]',
      sources: [
        {
          kind: 'web',
          title: 'Example research',
          url: 'https://example.com/research',
          domain: 'example.com',
          read_at: '2026-11-02T12:00:00Z',
          tool: 'web_read',
        },
        {
          kind: 'web',
          title: 'Second page',
          url: 'https://example.org/second',
          read_at: '2026-11-02T12:01:00Z',
        },
        {
          n: 1,
          kind: 'document',
          title: 'Design handbook',
          document_id: 'doc-1',
          chunk_id: 'chunk-1',
          page: 4,
          paragraph: 2,
          excerpt: 'Supporting evidence from the handbook.',
          blob_digest: 'a'.repeat(64),
        },
      ],
    },
  ])
  await page.route('https://**/favicon.ico', (route) => route.abort())
  await page.goto('/agents/excalibur?view=chat')
  const trigger = page.getByRole('button', { name: 'Fuentes (3)', exact: true })
  await trigger.click()
  const dialog = page.getByRole('dialog', { name: 'Fuentes (3)' })
  await expect(dialog.getByRole('listitem')).toHaveCount(3)
  await expect(dialog.getByRole('link', { name: 'Example research' })).toHaveAttribute(
    'target',
    '_blank',
  )
  await expect(dialog).toContainText('example.com')
  await expect(dialog).toContainText('p. 4')
  await expect(dialog).toContainText('párr. 2')
  await expect(dialog).toContainText('Supporting evidence')
  await expect(dialog.locator('time')).toHaveCount(2)
  for (let i = 0; i < 10; i++) {
    await page.keyboard.press('Tab')
    expect(await dialog.evaluate((node) => node.contains(document.activeElement))).toBe(true)
  }
  await page.keyboard.press('Escape')
  await expect(dialog).toHaveCount(0)
  await expect(trigger).toBeFocused()
})

test('live tools, notices, inline questions and plan approvals use typed RPC methods', async ({
  page,
}) => {
  const chat = await mockChat(page)
  const decisions: { method: string; params: unknown }[] = []
  await page.route('**/rpc', async (route) => {
    const request = route.request().postDataJSON()
    if (!['question.answer', 'plan.approve', 'plan.deny'].includes(request.method))
      return route.fallback()
    decisions.push(request)
    await route.fulfill({ json: { jsonrpc: '2.0', id: request.id, result: {} } })
  })
  await page.goto('/agents/excalibur?view=chat')
  await expect(page.locator('.chat-connection')).toHaveCount(0)
  await expect.poll(chat.connected).toBe(true)
  chat.emit('run.progress', { state: 'running' })
  chat.emit('tool.called', { tool: 'web_search', status: 'started' })
  await expect(page.getByText('Buscando en la web…', { exact: true })).toBeVisible()
  chat.emit('tool.completed', { tool: 'web_search', status: 'completed' })
  chat.emit('tool.called', { tool: 'web_read', status: 'started' })
  await expect(page.getByText('Leyendo página…', { exact: true })).toBeVisible()
  chat.emit('notice.posted', { notice_id: 'n1', content: 'Research is ready' })
  chat.emit('question.asked', { question_id: 'q1', content: 'Which environment?' })
  await page.getByRole('textbox', { name: 'Responder', exact: true }).fill('Staging')
  await page.getByRole('button', { name: 'Responder', exact: true }).click()
  chat.emit('plan.approval_requested', { plan_id: 'p1', summary: 'Deploy to staging' })
  await page.getByRole('button', { name: 'Aprobar', exact: true }).click()
  chat.emit('plan.approval_requested', { plan_id: 'p2', summary: 'Deploy to production' })
  await page.getByRole('button', { name: 'Rechazar', exact: true }).click()
  await expect
    .poll(() => decisions.map(({ method, params }) => ({ method, params })))
    .toEqual([
      { method: 'question.answer', params: { question_id: 'q1', answer: 'Staging' } },
      { method: 'plan.approve', params: { plan_id: 'p1' } },
      { method: 'plan.deny', params: { plan_id: 'p2' } },
    ])
  await expect(page.getByText('Research is ready')).toBeVisible()
})

test('failed sends retry with the same client ID and reconnecting preserves real history', async ({
  page,
}) => {
  const chat = await mockChat(page, [{ text: 'An existing real answer' }])
  const clientIds: string[] = []
  await page.route('**/rpc', async (route) => {
    const request = route.request().postDataJSON()
    if (request.method !== 'conversation.send') return route.fallback()
    clientIds.push(request.params.client_msg_id)
    if (clientIds.length > 1) return route.fallback()
    await route.fulfill({
      json: {
        jsonrpc: '2.0',
        id: request.id,
        error: { code: -32603, message: 'Temporary send failure' },
      },
    })
  })
  await page.goto('/agents/excalibur?view=chat')
  const input = page.getByRole('textbox', { name: 'Escribe a Excalibur…' })
  await input.fill('Retry this question')
  await input.press('Enter')
  await expect(page.getByText('Temporary send failure')).toBeVisible()
  await page.locator('.message-user').getByRole('button', { name: 'Reintentar' }).click()
  await expect(page.getByText('Temporary send failure')).toHaveCount(0)
  expect(clientIds).toHaveLength(2)
  expect(clientIds[0]).toBe(clientIds[1])
  await expect(page.locator('.message-user')).toHaveCount(1)
  chat.disconnect()
  await expect(page.locator('.chat-body .chat-connection').first()).toContainText('Reconectando')
  await expect(page.getByText('An existing real answer')).toBeVisible()
  await expect(page.locator('.chat-body .chat-connection')).toHaveCount(0)
  await expect(page.getByText('An existing real answer')).toHaveCount(1)
})

for (const width of [1440, 390]) {
  test(`redesigned chat suggestions, copy and reduced motion at ${width}px`, async ({
    page,
    context,
  }, testInfo) => {
    await context.grantPermissions(['clipboard-read', 'clipboard-write'])
    await page.setViewportSize({ width, height: width === 390 ? 844 : 900 })
    await page.emulateMedia({ reducedMotion: 'reduce', colorScheme: 'light' })
    await page.addInitScript(() => localStorage.setItem('excalibur-ui-theme', 'light'))
    const chat = await mockChat(page)
    await page.goto('/agents/excalibur?view=chat')
    await expect(page.locator('.chat-empty h2')).toBeVisible()
    await expect(page.locator('.chat-empty')).toHaveCSS('animation-name', 'none')
    await page.screenshot({ path: testInfo.outputPath(`chat-empty-${width}.png`) })
    const suggestion = page.locator('.chat-suggestions button').first()
    const prompt = await suggestion.innerText()
    await suggestion.click()
    await expect(page.locator('.message-user')).toContainText(prompt)
    expect(chat.messages.at(-1)?.text).toBe(prompt)
    chat.emit('message.created', {
      message_id: 'design-answer',
      conversation_id: 'cnv_web_excalibur',
      role: 'assistant',
      text: '## A little clarity\n\nHere is your next step.\n\n> Make room for focused work.\n\n| Step | Status |\n| --- | --- |\n| Review your plan | Ready |\n\n```typescript\nconst ready = true\n```',
      sources: [
        {
          kind: 'web',
          title: 'Planning guide',
          url: 'https://example.com/guide',
          domain: 'example.com',
        },
      ],
    })
    const copy = page.getByRole('button', { name: 'Copiar mensaje', exact: true })
    await copy.click()
    await expect(copy).toContainText('Copiado')
    expect(await page.evaluate(() => navigator.clipboard.readText())).toContain(
      '## A little clarity',
    )
    await expect(page.locator('.message-content').last()).toHaveCSS('animation-name', 'none')
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    await page.screenshot({ path: testInfo.outputPath(`chat-messages-${width}.png`) })
    await page.getByRole('button', { name: 'Fuentes (1)', exact: true }).click()
    await page.screenshot({ path: testInfo.outputPath(`chat-sources-${width}.png`) })
    await page.keyboard.press('Escape')
    if (width === 390) {
      await page.getByRole('button', { name: 'Más opciones', exact: true }).click()
    }
    await page
      .getByRole(width === 390 ? 'menuitem' : 'button', { name: 'Llamar', exact: true })
      .click()
    await expect(page.getByRole('dialog')).toBeVisible()
    await expect(page.locator('.call-orb-core .pixel-avatar')).toHaveCSS('width', '132px')
    await page.screenshot({ path: testInfo.outputPath(`chat-call-${width}.png`) })
    await page.keyboard.press('Escape')
    await page.evaluate(() => document.documentElement.classList.add('dark'))
    await expect(page.locator('.chat-body')).toHaveCSS('background-color', 'rgb(7, 11, 20)')
    await page.screenshot({ path: testInfo.outputPath(`chat-dark-${width}.png`) })
  })
}
