import { test, expect } from './fixtures'
import { labSession, mockRobotLab } from './robotlab-fixture'
import type { Session } from '../src/features/robotlab/types'

test.use({
  launchOptions: {
    args: [
      '--use-fake-ui-for-media-stream',
      '--use-fake-device-for-media-stream',
      '--autoplay-policy=no-user-gesture-required',
    ],
  },
  permissions: ['microphone'],
})

function resumed(session: Session): Session {
  const n = session.attempt + 1
  return {
    ...session,
    status: 'live',
    ended_at: null,
    outcome: null,
    attempt: n,
    max_attempts: session.attempts.length + 40,
    attempts: [
      ...session.attempts,
      {
        n,
        status: 'running',
        reason: '',
        started_at: '2026-11-04T00:00:00Z',
        ended_at: null,
        sim_time: 0,
        frames: 0,
        skills_used: ['fetch'],
      },
    ],
  }
}

test('resume from chat focuses the existing arm session, then the existing dog session', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  const { bus, calls, sessions } = await mockRobotLab(page)
  const shelf: Session = {
    ...labSession('shelf-training', 'stopped'),
    robot: 'arm',
    name: 'Box and shelf',
  }
  const stairs: Session = { ...labSession('stairs-training', 'exhausted'), name: 'Stairs' }
  sessions.set(shelf.id, shelf)
  sessions.set(stairs.id, stairs)
  const requests = [
    {
      text: 'Vuelve a intentar la simulación del brazo robótico en la sesión de la caja y el estante',
      session: shelf,
    },
    {
      text: 'Ahora vuelve a intentarlo con el perro, en la sesión de la escalera',
      session: stairs,
    },
  ]
  // These events stand in for the agent's sessions -> session_resume -> skill_use loop.
  // Discovery, mutation, skill preservation and ownership are exercised in backend tests.
  await page.route('**/rpc', async (route) => {
    const request = route.request().postDataJSON()
    const match = requests.find((item) => item.text === request.params?.text)
    if (request.method === 'conversation.send' && match) {
      for (const session of sessions.values())
        if (session.status === 'live') session.status = 'stopped'
      const next = resumed(match.session)
      sessions.set(next.id, next)
      bus.emit('robotlab.session', { session: next })
      bus.emit('robotlab.focus', {
        session: next.id,
        robot: next.robot,
        environment: next.environment,
      })
    }
    await route.fallback()
  })
  await page.goto('/agents/excalibur?view=chat')
  await bus.ready('robotlab.focus')
  for (const request of requests) {
    const composer = page.locator('.composer textarea').first()
    await composer.fill(request.text)
    await composer.press('Enter')
    await expect(page).toHaveURL(new RegExp(`/lab\\?tab=trainings&session=${request.session.id}`))
    await expect(page.locator('.lab-agent .composer textarea')).toBeVisible()
    const attempt = page.getByRole('combobox', { name: 'Attempt', exact: true })
    await expect(attempt).toHaveValue('3')
    await expect(attempt.locator('option')).toHaveCount(3)
    await expect(page.getByRole('button', { name: 'Stop session' })).toBeVisible()
    await expect(page.getByRole('button', { name: /Play/ })).toHaveCount(0)
    await page.getByRole('tab', { name: 'Skills', exact: true }).click()
    await expect(page.getByText('fetch', { exact: true })).toBeVisible()
    await page.getByRole('tab', { name: 'Chat', exact: true }).click()
  }
  expect(sessions.size).toBe(3)
  expect(
    calls.filter((call) => call.method === 'conversation.send').map((call) => call.params.text),
  ).toEqual(requests.map((request) => request.text))
  expect(calls.some((call) => call.method === 'robotlab.session.start')).toBe(false)
  await page.getByRole('button', { name: 'Back', exact: true }).click()
  const row = page
    .getByRole('row')
    .filter({ has: page.getByRole('button', { name: 'Stairs', exact: true }) })
  await expect(row).toContainText('Live')
  await expect(row).toContainText('3')
})

test('resume focus changes an open old replay to live without recreating the session', async ({
  page,
}) => {
  const { bus, sessions } = await mockRobotLab(page)
  await page.goto('/lab?tab=trainings&session=old-training')
  const attempt = page.getByRole('combobox', { name: 'Attempt', exact: true })
  await attempt.selectOption('1')
  await expect(page.getByRole('button', { name: /Play/ })).toBeVisible()
  await bus.ready('robotlab.focus')
  const next = resumed(sessions.get('old-training')!)
  sessions.set(next.id, next)
  bus.emit('robotlab.session', { session: next })
  bus.emit('robotlab.focus', { session: next.id, robot: next.robot, environment: next.environment })
  await expect(attempt).toHaveValue('3')
  await expect(attempt).toBeDisabled()
  await expect(page.getByRole('button', { name: /Play/ })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Stop session' })).toBeVisible()
  await page.getByRole('button', { name: 'Stop session' }).click()
  await expect(attempt).toBeEnabled()
  await expect(attempt).toHaveValue('3')
  await attempt.selectOption('1')
  await expect(page.getByRole('button', { name: /Play/ })).toBeVisible()
  expect(sessions.size).toBe(1)
})

test.describe('resume during a voice call', () => {
  test('focus from another page preserves the active call', async ({ page }) => {
    await page.addInitScript(() => {
      const create = AudioContext.prototype.createAnalyser
      AudioContext.prototype.createAnalyser = function () {
        const analyser = create.call(this)
        analyser.getFloatTimeDomainData = (samples: Float32Array<ArrayBuffer>) => samples.fill(0)
        return analyser
      }
    })
    await page.setViewportSize({ width: 1440, height: 900 })
    const { bus, sessions } = await mockRobotLab(page)
    await page.route('**/rpc', async (route) => {
      const request = route.request().postDataJSON()
      if (request.method !== 'voice.status') return route.fallback()
      await route.fulfill({
        json: { jsonrpc: '2.0', id: request.id, result: { available: true, voices: [] } },
      })
    })
    await page.goto('/agents/excalibur?view=chat')
    await page.getByRole('button', { name: 'Call', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: 'Call with Excalibur' })
    await expect(dialog.getByText('Listening', { exact: true })).toBeVisible()
    await bus.ready('robotlab.focus')
    const next = resumed(sessions.get('old-training')!)
    sessions.set(next.id, next)
    bus.emit('robotlab.session', { session: next })
    bus.emit('robotlab.focus', {
      session: next.id,
      robot: next.robot,
      environment: next.environment,
    })
    await expect(page).toHaveURL(/\/lab\?tab=trainings&session=old-training/)
    // The same global call remains available in both the lab sheet and composer.
    await expect(dialog.getByText('Listening', { exact: true })).toBeVisible()
    await dialog.getByRole('button', { name: 'Close', exact: true }).click()
    await expect(
      page.locator('.lab-agent').getByRole('button', { name: 'Hang up', exact: true }),
    ).toBeVisible()
    await page.locator('.lab-agent').getByRole('button', { name: 'Show call', exact: true }).click()
    await expect(dialog.getByText('Listening', { exact: true })).toBeVisible()
    await dialog.getByRole('button', { name: 'Close', exact: true }).click()
    await expect(
      page.locator('.lab-agent').getByRole('button', { name: 'Hang up', exact: true }),
    ).toBeVisible()
    await expect(page.getByRole('combobox', { name: 'Attempt', exact: true })).toHaveValue('3')
    await page.locator('.lab-agent').getByRole('button', { name: 'Hang up', exact: true }).click()
  })
})
