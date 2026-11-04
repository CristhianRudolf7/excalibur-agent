import { test, expect, type Page } from '@playwright/test'
import type { CredentialRequest } from '../src/lib/rpc/credentials'
import { defaultAgent } from '../src/features/agents/store'
import { mockEvents } from './event-fixture'

const initial: CredentialRequest = {
  id: 'credential-e2e',
  conversation_id: 'cnv_telegram_123',
  site: 'accounts.example.org',
  purpose: 'Sign in to review your account',
  fields: ['username', 'password'],
  remember_default: true,
  status: 'pending',
  created_at: '2026-11-03T12:00:00Z',
}
async function mock(
  page: Page,
  options: { locked?: boolean; pending?: boolean; language?: string; personal?: boolean } = {},
) {
  const events = await mockEvents(page)
  const personal = {
    ...defaultAgent,
    id: 'pablito',
    name: 'Pablito',
    favorite: false,
    conversation_id: 'cnv_agent_pablito',
  }
  let requests =
    options.pending === false
      ? []
      : [
          {
            ...initial,
            conversation_id: options.personal ? personal.conversation_id : initial.conversation_id,
          },
        ]
  let entries = [
    {
      id: 'saved-1',
      name: 'example-login',
      type: 'website_password',
      domains: ['accounts.example.org'],
      metadata: {},
      created_at: initial.created_at,
      updated_at: initial.created_at,
    },
  ]
  let locked = options.locked ?? false
  let submissions = 0
  let unlocks = 0
  let deletes = 0
  const historyTargets: string[] = []
  const chatMessages: string[] = []
  await page.addInitScript((language) => {
    localStorage.setItem('excalibur-ui-language', language)
    localStorage.setItem('world-last-mode', 'world')
  }, options.language ?? 'en')
  await page.route('**/rpc', async (route) => {
    const request = route.request().postDataJSON()
    let result: unknown = {}
    let error: unknown
    switch (request.method) {
      case 'agents.list':
        result = {
          agents: options.personal ? [defaultAgent, personal] : [defaultAgent],
          favorite_id: defaultAgent.id,
        }
        break
      case 'agents.mark_read':
        result = { unread: 0 }
        break
      case 'principal.get':
        result = { id: 'local', name: 'Example User', language: 'en', timezone: 'UTC' }
        break
      case 'activity.list':
        result = { items: [] }
        break
      case 'conversation.history':
        historyTargets.push(request.params.conversation_id)
        result = {
          messages: [
            {
              id: 'history-1',
              conversation_id: request.params.conversation_id,
              role: 'assistant',
              text: 'Open the secure credential form to continue.',
            },
          ],
        }
        break
      case 'conversation.send':
        chatMessages.push(request.params.text)
        result = { msg_id: 'sent' }
        break
      case 'credentials.pending':
        result = {
          requests: requests.filter(
            (item) =>
              !request.params.conversation_id ||
              item.conversation_id === request.params.conversation_id,
          ),
        }
        break
      case 'credentials.submit':
        if (locked)
          error = { code: -32099, message: 'vault_locked', data: { code: 'vault_locked' } }
        else {
          expect(request.params.values).toEqual({
            username: 'e2e-username',
            password: 'e2e-private-value',
          })
          expect(request.params.remember).toBe(true)
          submissions++
          requests = []
          result = { ok: true }
        }
        break
      case 'credentials.cancel':
        requests = []
        result = { ok: true }
        break
      case 'vault.unlock':
        expect(request.params.passphrase).toBe('e2e-unlock-phrase')
        locked = false
        unlocks++
        result = { unlocked: true }
        break
      case 'vault.lock':
        locked = true
        result = { locked: true }
        break
      case 'vault.list_metadata':
        result = { entries, locked }
        break
      case 'vault.delete':
        expect(request.params.confirmed).toBe(true)
        entries = []
        deletes++
        result = { deleted: request.params.name }
        break
    }
    await route.fulfill({
      json: { jsonrpc: '2.0', id: request.id, ...(error ? { error } : { result }) },
    })
  })
  return {
    events,
    historyTargets,
    chatMessages,
    submissions: () => submissions,
    unlocks: () => unlocks,
    deletes: () => deletes,
  }
}

for (const viewport of [
  { width: 1280, height: 900 },
  { width: 390, height: 844 },
]) {
  test(`credential deep link unlocks and submits outside chat at ${viewport.width}px`, async ({
    page,
  }) => {
    await page.setViewportSize(viewport)
    const api = await mock(page, { locked: true })
    await page.goto('/?credential=credential-e2e')
    const card = page.getByRole('region', { name: 'Credentials for accounts.example.org' })
    await expect(card).toBeVisible()
    await expect(card).toBeFocused()
    await page.screenshot({ path: test.info().outputPath('credential-pending.png') })
    expect(api.historyTargets).toContain(initial.conversation_id)
    await expect(page).toHaveURL(/view=chat/)
    await card.getByLabel('Username', { exact: true }).fill('e2e-username')
    await card.getByLabel('Password', { exact: true }).fill('e2e-private-value')
    await card.getByRole('button', { name: 'Show Password', exact: true }).click()
    await expect(card.getByLabel('Password', { exact: true })).toHaveAttribute('type', 'text')
    await card.getByRole('button', { name: 'Hide Password', exact: true }).click()
    await card.getByRole('button', { name: 'Send', exact: true }).click()
    const phrase = card.getByLabel('Vault passphrase', { exact: true })
    await phrase.fill('e2e-unlock-phrase')
    await card.getByRole('button', { name: 'Unlock', exact: true }).click()
    await expect(card).toContainText('Credentials sent securely')
    await expect(card.locator('input')).toHaveCount(0)
    expect(api.submissions()).toBe(1)
    expect(api.unlocks()).toBe(1)
    expect(api.chatMessages).toEqual([])
    const surface = await page.evaluate(() =>
      JSON.stringify({
        html: document.body.innerHTML,
        storage: { ...localStorage },
        session: { ...sessionStorage },
        url: location.href,
      }),
    )
    expect(surface).not.toContain('e2e-private-value')
    expect(surface).not.toContain('e2e-unlock-phrase')
    expect(surface).not.toContain('e2e-username')
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  })
}

test('requested events show a Spanish card and external resolution clears its input', async ({
  page,
}) => {
  const api = await mock(page, { pending: false, language: 'es' })
  await page.goto('/agents/excalibur?view=chat')
  // The credential card and the live browser panel both subscribe.
  await api.events.ready('credentials.requested', 2)
  api.events.emit('credentials.requested', {
    ...initial,
    conversation_id: 'cnv_web_excalibur',
    request_id: initial.id,
    run_id: 'run-e2e',
  })
  const card = page.getByRole('region', { name: 'Credenciales para accounts.example.org' })
  await expect(card).toBeVisible()
  await card.getByLabel('Contraseña', { exact: true }).fill('external-resolution-value')
  api.events.emit('credentials.resolved', { request_id: initial.id, status: 'cancelled' })
  await expect(card).toContainText('Cancelado')
  await expect(card.locator('input')).toHaveCount(0)
  expect(await page.content()).not.toContain('external-resolution-value')
})

test('password settings list metadata, confirm deletion and fit a mobile viewport', async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 })
  const api = await mock(page)
  await page.goto('/settings?section=passwords')
  await expect(page.getByText('Vault unlocked', { exact: true })).toBeVisible()
  await expect(page.getByText('accounts.example.org', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Delete example-login', exact: true }).click()
  expect(api.deletes()).toBe(0)
  await page.getByRole('dialog').getByRole('button', { name: 'Cancel', exact: true }).click()
  expect(api.deletes()).toBe(0)
  await page.getByRole('button', { name: 'Delete example-login', exact: true }).click()
  await page.getByRole('dialog').getByRole('button', { name: 'Delete', exact: true }).click()
  await expect(page.getByText('No remembered sites.')).toBeVisible()
  expect(api.deletes()).toBe(1)
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
})

test('an arriving card stays visible at the bottom of a long conversation', async ({ page }) => {
  const api = await mock(page, { pending: false })
  await page.route('**/rpc', async (route) => {
    const request = route.request().postDataJSON()
    if (request.method !== 'conversation.history') return route.fallback()
    await route.fulfill({
      json: {
        jsonrpc: '2.0',
        id: request.id,
        result: {
          messages: Array.from({ length: 40 }, (_, index) => ({
            id: `history-${index}`,
            conversation_id: 'cnv_web_excalibur',
            role: 'assistant',
            text: `Conversation entry ${index}: review the account before continuing.`,
          })),
        },
      },
    })
  })
  await page.goto('/agents/excalibur?view=chat')
  // The credential card and the live browser panel both subscribe.
  await api.events.ready('credentials.requested', 2)
  await expect(page.locator('.message').first()).toBeVisible()
  await page.locator('.messages').evaluate((node) => {
    node.scrollTop = node.scrollHeight
    node.dispatchEvent(new Event('scroll'))
  })
  api.events.emit('credentials.requested', {
    ...initial,
    conversation_id: 'cnv_web_excalibur',
    request_id: initial.id,
    run_id: 'run-e2e',
  })
  await expect(
    page.getByRole('region', { name: 'Credentials for accounts.example.org' }),
  ).toBeInViewport()
})

test('credential cards stay in the requesting agent chat and its unread badge updates', async ({
  page,
}) => {
  const api = await mock(page, { personal: true })
  await page.goto('/agents/excalibur?view=chat')
  await api.events.ready('agents.unread')
  await expect(
    page.getByRole('region', { name: 'Credentials for accounts.example.org' }),
  ).toHaveCount(0)
  api.events.emit('agents.unread', {
    agent_id: 'pablito',
    unread: 1,
    last_preview: 'Secure credentials requested',
    last_message_at: initial.created_at,
  })
  const row = page
    .locator('.personal-agent-nav')
    .getByRole('button', { name: 'Pablito', exact: true })
  await expect(row.getByLabel('1 unread')).toBeVisible()
  await row.click()
  await expect(page.getByRole('button', { name: 'Switch agent: Pablito' })).toBeVisible()
  await expect(
    page.getByRole('region', { name: 'Credentials for accounts.example.org' }),
  ).toBeVisible()
  expect(api.historyTargets).toContain('cnv_agent_pablito')
  await page.goto('/agents/excalibur?credential=credential-e2e&conversation=cnv_agent_pablito')
  await expect(page).toHaveURL(/agents\/pablito/)
  await expect(page.getByRole('button', { name: 'Switch agent: Pablito' })).toBeVisible()
  await expect(
    page.getByRole('region', { name: 'Credentials for accounts.example.org' }),
  ).toBeFocused()
})

for (const theme of ['light', 'dark']) {
  test(`password settings save a grouped login securely on mobile in ${theme} theme`, async ({
    page,
  }) => {
    await page.setViewportSize({ width: 390, height: 844 })
    await mock(page, { pending: false })
    let saved = false
    let valid = false
    await page.addInitScript((theme) => localStorage.setItem('excalibur-ui-theme', theme), theme)
    await page.route('**/rpc', async (route) => {
      const request = route.request().postDataJSON()
      if (request.method === 'credentials.save_login') {
        const p = request.params
        valid =
          p.site === 'accounts.example.org' &&
          p.start_url === 'https://start.example.org/home' &&
          p.label === 'Office' &&
          p.values.username === 'saved-user-sentinel' &&
          p.values.password === 'saved-password-sentinel'
        saved = true
        await route.fulfill({ json: { jsonrpc: '2.0', id: request.id, result: { ok: true } } })
        return
      }
      if (request.method === 'vault.list_metadata') {
        const entries = saved
          ? ['username', 'password'].map((field) => ({
              id: `saved-${field}`,
              name: `credentials-bundle-${field}`,
              type: 'website_password',
              domains: ['accounts.example.org'],
              created_at: initial.created_at,
              updated_at: initial.created_at,
              metadata: {
                credential_bundle: 'credentials-bundle',
                credential_label: 'Office',
                credential_site: 'accounts.example.org',
                credential_field: field,
              },
            }))
          : []
        await route.fulfill({
          json: { jsonrpc: '2.0', id: request.id, result: { entries, locked: false } },
        })
        return
      }
      await route.fallback()
    })
    await page.goto('/settings?section=passwords')
    await expect(page.getByText('Vault unlocked', { exact: true })).toBeVisible()
    await page.getByRole('button', { name: 'Add login', exact: true }).click()
    const dialog = page.getByRole('dialog')
    await dialog.getByLabel('Name (optional)', { exact: true }).fill('Office')
    await dialog.getByLabel('Address', { exact: true }).fill('https://start.example.org/home')
    await dialog
      .getByLabel('Login host (optional)', { exact: true })
      .fill('ACCOUNTS.example.org:443/login')
    expect(await dialog.evaluate((node) => node.scrollWidth <= node.clientWidth)).toBe(true)
    await dialog.getByLabel('Username', { exact: true }).fill('saved-user-sentinel')
    await dialog.getByLabel('Password', { exact: true }).fill('saved-password-sentinel')
    await dialog.getByRole('button', { name: 'Save login', exact: true }).click()
    await expect(dialog).toHaveCount(0)
    expect(valid).toBe(true)
    await expect(page.getByText('Office', { exact: true })).toHaveCount(1)
    await expect(page.getByText('accounts.example.org', { exact: true })).toHaveCount(1)
    await expect(page.getByText('Username saved', { exact: true })).toBeVisible()
    const surface = await page.evaluate(() =>
      JSON.stringify({
        html: document.body.innerHTML,
        values: [...document.querySelectorAll('input')].map((input) => input.value),
        local: { ...localStorage },
        session: { ...sessionStorage },
      }),
    )
    expect(
      surface.includes('saved-password-sentinel') || surface.includes('saved-user-sentinel'),
    ).toBe(false)
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    await page.screenshot({ path: test.info().outputPath(`saved-login-${theme}.png`) })
  })
}
