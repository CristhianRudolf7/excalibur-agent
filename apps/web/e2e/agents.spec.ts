import { test, expect } from './fixtures'
import { mockAgents } from './agents-fixture'

test('waits for the registry before loading a deep-linked agent conversation', async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('excalibur-ui-language', 'en'))
  const fixture = await mockAgents(page)
  let release!: () => void
  const registry = new Promise<void>((resolve) => {
    release = resolve
  })
  await page.route('**/rpc', async (route) => {
    if (route.request().postDataJSON().method === 'agents.list') await registry
    return route.fallback()
  })
  await page.goto('/agents/sam?view=chat')
  try {
    await expect(page.locator('.sk-agent-header')).toBeVisible({ timeout: 2000 })
    await expect(page.getByRole('button', { name: 'Create agent', exact: true })).toBeDisabled()
    expect(fixture.requests.filter((request) => request.method === 'conversation.history')).toEqual(
      [],
    )
    await expect(page.getByRole('textbox')).toHaveCount(0)
  } finally {
    release()
  }
  await expect(page.locator('.message-history')).toContainText('Sam private history')
  await expect(page.getByRole('button', { name: 'Create agent', exact: true })).toBeEnabled()
  await expect(page.locator('.message-history')).not.toContainText('Ada private history')
  expect(
    fixture.requests
      .filter((request) => request.method === 'conversation.history')
      .every((request) => request.params?.conversation_id === 'cnv_agent_sam'),
  ).toBe(true)
})

for (const language of ['en', 'es']) {
  test(`agent chats, unread events and unsent creation prefill in ${language}`, async ({
    page,
  }) => {
    await page.addInitScript((lng) => localStorage.setItem('excalibur-ui-language', lng), language)
    const fixture = await mockAgents(page)
    await page.goto('/')
    await expect(page).toHaveURL(/\/agents\/ada/)
    const sidebar = page.locator('.personal-agent-nav')
    await expect(sidebar.getByRole('button').first()).toHaveAttribute('aria-label', 'Ada')
    await expect(sidebar.getByRole('button')).toHaveCount(2)
    await expect(page.locator('.message-history')).toContainText('Ada private history')
    await expect(page.locator('.message-history')).not.toContainText('Sam private history')
    await fixture.bus.ready('agents.unread')
    fixture.unread('sam', 5, 'A new response')
    await expect(sidebar.getByRole('button', { name: 'Sam', exact: true })).toContainText('5')
    await expect(sidebar).not.toContainText('A new response')
    await sidebar.getByRole('button', { name: 'Sam', exact: true }).click()
    await expect(page.locator('.message-history')).toContainText('Sam private history')
    await expect(page.locator('.message-history')).not.toContainText('Ada private history')
    await expect(page.locator('.agent-relayed')).toHaveText(
      language === 'en' ? 'sent by Ada' : 'enviado por Ada',
    )
    await expect(
      sidebar.getByRole('button', { name: 'Sam', exact: true }).locator('.unread'),
    ).toHaveCount(0)
    expect(
      fixture.requests.some(
        (request) => request.method === 'agents.mark_read' && request.params?.id === 'sam',
      ),
    ).toBe(true)
    await page
      .getByRole('button', {
        name: language === 'en' ? 'Create agent' : 'Crear agente',
        exact: true,
      })
      .click()
    await expect(page).toHaveURL(/\/agents\/ada/)
    const composer = page.getByRole('textbox')
    await expect(composer).toHaveValue(
      language === 'en'
        ? 'Hi, can you create an additional agent, please?'
        : 'Hola, ¿puedes crear un agente adicional, por favor?',
    )
    await expect(composer).toBeFocused()
    expect(fixture.requests.some((request) => request.method === 'conversation.send')).toBe(false)
    // An explicit send still uses the favorite's server-provided legacy conversation ID.
    await composer.press('Enter')
    await expect
      .poll(
        () =>
          fixture.requests.find((request) => request.method === 'conversation.send')?.params
            ?.conversation_id,
      )
      .toBe('cnv_web_excalibur')
  })
}

test('keyboard switcher works in tool headers and narrow screens show only the avatar', async ({
  page,
}) => {
  await page.addInitScript(() => localStorage.setItem('excalibur-ui-language', 'en'))
  const fixture = await mockAgents(page)
  await page.goto('/agents/ada?view=chat')
  const switcher = page.getByRole('button', { name: 'Switch agent: Ada' })
  await switcher.focus()
  await page.keyboard.press('Enter')
  await expect(page.getByRole('menuitemradio').first()).toHaveAttribute('aria-label', 'Ada')
  await page.getByRole('menuitemradio', { name: 'Sam', exact: true }).click()
  await expect(page).toHaveURL(/\/agents\/sam/)
  await page.getByRole('button', { name: 'Toggle panel', exact: true }).click()
  const panel = page.locator('.right-panel')
  await expect(panel.getByRole('button', { name: 'Switch agent: Sam' })).toBeVisible()
  await panel.getByRole('button', { name: 'Switch agent: Sam' }).click()
  await page.getByRole('menuitemradio', { name: 'Ada', exact: true }).click()
  await expect(page).toHaveURL(/\/agents\/ada/)
  await page.goto('/robot')
  await expect(page.getByRole('button', { name: 'Switch agent: Ada' })).toBeVisible()
  await page.setViewportSize({ width: 390, height: 844 })
  const mobileSwitcher = page.getByRole('button', { name: 'Switch agent: Ada' })
  await expect(mobileSwitcher).toBeVisible()
  await expect(mobileSwitcher.locator('.agent-switcher-name')).toBeHidden()
  await expect(mobileSwitcher.locator('.pixel-avatar')).toBeVisible()
  expect(fixture.requests.some((request) => request.method === 'agents.list')).toBe(true)
})

test('document, spreadsheet, whiteboard and diagram panel headers retain their view when switching', async ({
  page,
}) => {
  await page.addInitScript(() => localStorage.setItem('excalibur-ui-language', 'en'))
  await mockAgents(page)
  for (const panel of ['document', 'sheet', 'whiteboard', 'diagrams']) {
    await page.goto(`/agents/ada?view=chat&panel=${panel}&panelDeepLink=1`)
    await page.getByRole('button', { name: 'Toggle panel', exact: true }).click()
    const header = page.locator('.right-panel .panel-navigation')
    await expect(page.locator(`#panel-${panel}`)).toBeVisible()
    await header.getByRole('button', { name: 'Switch agent: Ada' }).click()
    await page.getByRole('menuitemradio', { name: 'Sam', exact: true }).click()
    await expect(page).toHaveURL(/\/agents\/sam/)
    await expect(page.locator(`#panel-${panel}`)).toBeVisible()
    await expect(header.getByRole('button', { name: 'Switch agent: Sam' })).toBeVisible()
    await expect(page.locator('.message-history')).toContainText('Sam private history')
  }
})

test('classroom and robot lab side conversations use the agent selected in their headers', async ({
  page,
}) => {
  await page.addInitScript(() => localStorage.setItem('excalibur-ui-language', 'en'))
  const fixture = await mockAgents(page)
  await page.route('**/rpc', async (route) => {
    const { id, method } = route.request().postDataJSON()
    const result =
      method === 'classroom.list'
        ? { courses: [] }
        : method === 'robotlab.status'
          ? { installed: true }
          : method === 'robotlab.scenarios'
            ? { scenarios: [] }
            : undefined
    if (result === undefined) return route.fallback()
    return route.fulfill({ json: { jsonrpc: '2.0', id, result } })
  })
  for (const [path, chat] of [
    ['/classroom', 'Classroom conversation'],
    ['/lab', 'Agent chat'],
  ] as const) {
    await page.goto(path)
    await page.getByRole('button', { name: 'Switch agent: Ada' }).click()
    await page.getByRole('menuitemradio', { name: 'Sam', exact: true }).click()
    await page.getByRole('button', { name: chat, exact: true }).click()
    const conversation = page.locator('.classroom-chat, .lab-chat')
    await expect(conversation.locator('.message-history')).toContainText('Sam private history')
    await expect(conversation.locator('.message-history')).not.toContainText('Ada private history')
    const previousSends = fixture.requests.filter(
      (request) => request.method === 'conversation.send',
    ).length
    await conversation.getByRole('textbox').fill('Work with this tool')
    await conversation.getByRole('textbox').press('Enter')
    await expect
      .poll(
        () => fixture.requests.filter((request) => request.method === 'conversation.send').length,
      )
      .toBe(previousSends + 1)
    expect(
      fixture.requests.filter((request) => request.method === 'conversation.send').at(-1)?.params
        ?.conversation_id,
    ).toBe('cnv_agent_sam')
  }
})

test('agent settings edit, favorite hand-over, restore and avatar prefill', async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('excalibur-ui-language', 'en'))
  const fixture = await mockAgents(page)
  await page.goto('/settings?section=agents')
  const ada = page.getByRole('article', { name: 'Ada', exact: true })
  await ada.getByRole('button', { name: 'Archive', exact: true }).click()
  const confirm = page.getByRole('dialog', { name: 'Archive Ada' })
  await expect(confirm).toContainText('most recently used')
  expect(fixture.requests.some((request) => request.method === 'agents.archive')).toBe(false)
  await confirm.getByRole('button', { name: 'Archive', exact: true }).click()
  await expect(confirm).toHaveCount(0)
  const sam = page.getByRole('article', { name: 'Sam', exact: true })
  await expect(sam).toContainText('Favorite')
  await ada.getByRole('button', { name: 'Restore' }).click()
  await expect(ada.getByRole('button', { name: 'Edit', exact: true })).toBeVisible()
  await ada.getByRole('button', { name: 'Edit', exact: true }).click()
  const edit = page.getByRole('dialog', { name: 'Edit', exact: true })
  await edit.getByLabel('Agent name', { exact: true }).fill('Ada Lovelace')
  await edit.getByLabel('Personality and instructions').fill('Explain your reasoning concisely.')
  await edit.getByRole('button', { name: 'Save', exact: true }).click()
  const renamed = page.getByRole('article', { name: 'Ada Lovelace', exact: true })
  await renamed.getByRole('button', { name: 'Change avatar' }).click()
  const avatar = page.getByRole('dialog', { name: 'Change avatar' })
  await avatar.getByLabel('Describe the new avatar').fill('purple headphones')
  await avatar.getByRole('button', { name: 'Ask the favorite' }).click()
  await expect(page).toHaveURL(/\/agents\/sam/)
  await expect(page.getByRole('textbox')).toHaveValue(
    'Change the avatar of Ada Lovelace to purple headphones',
  )
  await expect(page.getByRole('textbox')).toBeFocused()
  expect(fixture.requests.some((request) => request.method === 'conversation.send')).toBe(false)
})
