import { test, expect, type Page } from './fixtures'
import { defaultAgent } from '../src/features/agents/store'
import { fallbackAvatar, paladinAvatar } from '../src/components/avatar-spec'
import type { Agent } from '../src/features/agents/types'

// The daemon's generated Excalibur seed, before the web Paladin migration.
const builtIn: Agent = {
  ...defaultAgent,
  id: 'agt_builtin',
  avatar: {
    variant: 'beard',
    tone: '#DCA879',
    hair: '#84613F',
    highlight: '#F7D5E9',
    outfit: '#9A6481',
    accent: '#B9D788',
    accessory: 'none',
    background: '#F5E8EE',
  },
  last_message_at: '2026-11-03T12:00:00Z',
  last_preview: 'I moved the gripper to the target',
  description: 'Built-in assistant description',
}
const longName = 'An agent with a very long name that must truncate on a single line'
function agentList(count: number): Agent[] {
  return [
    builtIn,
    ...Array.from({ length: count - 1 }, (_, index) => ({
      ...defaultAgent,
      id: `agent-${index}`,
      name: index === 0 ? longName : `Agent ${index}`,
      avatar: fallbackAvatar,
      favorite: false,
      conversation_id: `cnv_agent_${index}`,
      created_at: String(index).padStart(3, '0'),
      last_message_at: '2026-11-03T11:00:00Z',
      last_preview: 'A private preview that must not appear',
      unread: 3,
    })),
  ]
}
async function registry(page: Page, agents: Agent[]) {
  await page.addInitScript(() => localStorage.setItem('excalibur-ui-language', 'en'))
  await page.route('**/rpc', async (route) => {
    const { id, method } = route.request().postDataJSON()
    if (method !== 'agents.list') return route.fallback()
    return route.fulfill({
      json: { jsonrpc: '2.0', id, result: { agents, favorite_id: builtIn.id } },
    })
  })
}
async function rowsDoNotScroll(page: Page) {
  const rows = page.locator('.personal-agent-nav .agent-chat-row')
  expect(await rows.count()).toBeGreaterThan(0)
  expect(
    await rows.evaluateAll((nodes) =>
      nodes.every((node) => {
        const style = getComputedStyle(node)
        return (
          node.scrollHeight <= node.clientHeight &&
          node.scrollWidth <= node.clientWidth &&
          !['auto', 'scroll'].includes(style.overflowY) &&
          !['auto', 'scroll'].includes(style.overflowX) &&
          node.clientHeight === 44
        )
      }),
    ),
  ).toBe(true)
  await expect(page.locator('.agent-chat-preview')).toHaveCount(0)
  await expect(page.locator('.personal-agent-nav')).not.toContainText(builtIn.last_preview)
  await expect(page.locator('.personal-agent-nav')).not.toContainText(builtIn.description)
  await expect(page.locator('.personal-agent-nav')).not.toContainText('A private preview')
}

for (const width of [1440, 390]) {
  test(`compact sidebar rows have no internal scroll at ${width}px`, async ({ page }) => {
    await registry(page, agentList(10))
    await page.setViewportSize({ width, height: 900 })
    await page.goto('/agents/agt_builtin?view=chat')
    if (width < 1024) {
      await page
        .locator('.chat-header')
        .getByRole('button', { name: 'Toggle sidebar', exact: true })
        .click()
    }
    const list = page.locator('.agent-sidebar-scroll')
    await expect(list.getByRole('button')).toHaveCount(10)
    await rowsDoNotScroll(page)
    await expect(list).toHaveCSS('scrollbar-width', 'thin')
    expect(await list.evaluate((node) => node.scrollHeight > node.clientHeight)).toBe(true)
    const name = list.getByRole('button', { name: longName }).locator('.agent-chat-name')
    await expect(name).toHaveCSS('text-overflow', 'ellipsis')
    await expect(name).toHaveCSS('white-space', 'nowrap')
    expect(await name.evaluate((node) => node.scrollWidth > node.clientWidth)).toBe(true)
    const main = list.getByRole('button', { name: 'Excalibur', exact: true })
    await expect(main.locator('img')).toHaveAttribute('src', /\/paladin\.svg$/)
    const star = main.getByRole('img', { name: 'Favorite' })
    await expect(star).toHaveAttribute('title', 'Favorite')
    await expect(star.locator('svg')).toHaveCSS('width', '10px')
    await expect(star.locator('svg')).toHaveCSS('fill', 'none')
    await expect(main.locator('time')).toBeVisible()
    const other = list.getByRole('button', { name: longName })
    await expect(other.locator('.unread')).toHaveText('3')
    // Avatar, name, time and badge share one horizontal center line.
    expect(
      await other.evaluate((node) => {
        const centers = [
          ...node.querySelectorAll('.pixel-avatar, .agent-chat-name, time, .unread'),
        ].map((child) => {
          const rect = child.getBoundingClientRect()
          return rect.y + rect.height / 2
        })
        return Math.max(...centers) - Math.min(...centers) < 1
      }),
    ).toBe(true)
  })
}

test('large agent lists scroll and virtualize without scrolling their rows', async ({ page }) => {
  await registry(page, agentList(80))
  await page.goto('/agents/agt_builtin?view=chat')
  const list = page.locator('.agent-sidebar-scroll')
  await expect(list.getByRole('button', { name: 'Excalibur', exact: true })).toBeVisible()
  expect(await list.getByRole('button').count()).toBeLessThan(80)
  await rowsDoNotScroll(page)
  await list.evaluate((node) => {
    node.scrollTop = node.scrollHeight
  })
  await expect(list.getByRole('button', { name: 'Agent 78', exact: true })).toBeVisible()
  await rowsDoNotScroll(page)
})

test('legacy Excalibur uses Paladin in every shared avatar surface and preserves custom specs', async ({
  page,
}) => {
  const agents = [builtIn, { ...agentList(2)[1]!, avatar: paladinAvatar }]
  await registry(page, agents)
  await page.addInitScript(() => {
    navigator.mediaDevices.getUserMedia = () =>
      Promise.reject(new DOMException('Denied', 'NotAllowedError'))
  })
  await page.route('**/rpc', async (route) => {
    const { id, method } = route.request().postDataJSON()
    const result =
      method === 'conversation.history'
        ? {
            messages: [
              {
                id: 'assistant-1',
                conversation_id: 'cnv_web_excalibur',
                role: 'assistant',
                text: 'Paladin response',
              },
            ],
          }
        : method === 'voice.status'
          ? { available: false, voices: [] }
          : undefined
    if (!result) return route.fallback()
    return route.fulfill({ json: { jsonrpc: '2.0', id, result } })
  })
  await page.goto('/agents/agt_builtin?view=chat')
  await expect(page.locator('.chat-header .pixel-avatar img')).toHaveAttribute(
    'src',
    /\/paladin\.svg$/,
  )
  await expect(page.locator('.message-history .pixel-avatar img')).toHaveAttribute(
    'src',
    /\/paladin\.svg$/,
  )
  await page.getByRole('button', { name: 'Call', exact: true }).click()
  const call = page.getByRole('dialog', { name: 'Call with Excalibur' })
  await expect(call.locator('.pixel-avatar img')).toHaveAttribute('src', /\/paladin\.svg$/)
  await call.getByRole('button', { name: 'Hang up' }).click()
  await page.keyboard.press('Control+b')
  await expect(
    page.locator('.sidebar-rail-agents').getByRole('button', { name: 'Excalibur' }).locator('img'),
  ).toHaveAttribute('src', /\/paladin\.svg$/)
  await page.goto('/robot')
  const switcher = page.getByRole('button', { name: 'Switch agent: Excalibur' })
  await expect(switcher.locator('img')).toHaveAttribute('src', /\/paladin\.svg$/)
  await switcher.click()
  await expect(page.getByRole('menuitemradio', { name: longName }).locator('img')).toHaveAttribute(
    'src',
    /\/paladin\.svg$/,
  )
  await page.keyboard.press('Escape')
  await page.goto('/settings?section=agents')
  const card = page.getByRole('article', { name: 'Excalibur', exact: true })
  await expect(card.locator('.pixel-avatar img')).toHaveAttribute('src', /\/paladin\.svg$/)
  await card.getByRole('button', { name: 'Change avatar' }).click()
  const avatar = page.getByRole('dialog', { name: 'Change avatar' })
  await expect(avatar.locator('.pixel-avatar img')).toHaveAttribute('src', /\/paladin\.svg$/)
  // A confirmed replacement stored by Settings must survive a reload.
  agents[0] = { ...builtIn, avatar: { ...fallbackAvatar, variant: 'headphones' } }
  await page.reload()
  await expect(card.locator('.pixel-avatar svg')).toBeVisible()
  await expect(card.locator('.pixel-avatar img')).toHaveCount(0)
})
