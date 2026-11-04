import type { Page, WebSocketRoute } from '@playwright/test'
import { test, expect } from './fixtures'
import { mockRobotLab } from './robotlab-fixture'

type Event = { seq: number; id: string; type: string; at: string; payload: unknown }

async function holdLabSubscription(page: Page) {
  let pending: { socket: WebSocketRoute; id: string } | undefined
  let sequence = 0
  // Hold the initial response until the route is rendered, then replay the bus.
  await page.routeWebSocket('**/ws', (socket) => {
    socket.onMessage((data) => {
      const request = JSON.parse(String(data))
      if (request.method !== 'conversation.subscribe') return
      if (request.params.types.includes('robotlab.focus')) pending = { socket, id: request.id }
      else
        socket.send(
          JSON.stringify({ jsonrpc: '2.0', id: request.id, result: { events: [], cursor: '' } }),
        )
    })
  })
  return {
    event(type: string, payload: unknown, at = new Date().toISOString()): Event {
      return { seq: ++sequence, id: `replay-${sequence}`, type, payload, at }
    },
    async replay(events: Event[]) {
      await expect.poll(() => Boolean(pending)).toBe(true)
      pending!.socket.send(
        JSON.stringify({ jsonrpc: '2.0', id: pending!.id, result: { events, cursor: '' } }),
      )
    },
    live(event: Event) {
      pending!.socket.send(JSON.stringify({ jsonrpc: '2.0', method: 'event', params: event }))
    },
  }
}

for (const timestamp of ['old', 'missing'] as const) {
  test(`initial ${timestamp} focus cannot leave the trainings list; replay still refreshes it`, async ({
    page,
  }) => {
    const { sessions } = await mockRobotLab(page)
    const subscription = await holdLabSubscription(page)
    await page.goto('/lab?tab=trainings')
    await expect(page.getByRole('button', { name: 'Old training', exact: true })).toBeVisible()
    const session = { ...sessions.get('old-training')!, name: 'Replayed training' }
    sessions.set(session.id, session)
    const at = timestamp === 'old' ? new Date(Date.now() - 300_000).toISOString() : ''
    await subscription.replay([
      subscription.event(
        'robotlab.focus',
        { session: session.id, robot: session.robot, environment: session.environment },
        at,
      ),
      subscription.event('robotlab.session', { session }, at),
    ])
    // This updated row confirms the same replay batch reached the feature caches.
    await expect(page.getByRole('button', { name: 'Replayed training', exact: true })).toBeVisible()
    await expect(page).toHaveURL(/\/lab\?tab=trainings$/)
    subscription.live(
      subscription.event('robotlab.focus', {
        session: session.id,
        robot: session.robot,
        environment: session.environment,
      }),
    )
    await expect(page).toHaveURL(/\/lab\?tab=trainings&session=old-training/)
    await expect(page.locator('.lab-agent .composer textarea')).toBeVisible()
  })
}

test('recent focus in the initial replay opens the session from settings', async ({ page }) => {
  await mockRobotLab(page)
  const subscription = await holdLabSubscription(page)
  await page.goto('/settings?section=robotlab')
  await expect(page.getByLabel('Maximum attempts')).toHaveValue('40')
  await subscription.replay([
    subscription.event(
      'robotlab.focus',
      { session: 'old-training', robot: 'dog', environment: 'room' },
      new Date(Date.now() - 10_000).toISOString(),
    ),
  ])
  await expect(page).toHaveURL(/\/lab\?tab=trainings&session=old-training/)
  await expect(page.locator('.lab-agent .composer textarea')).toBeVisible()
})
