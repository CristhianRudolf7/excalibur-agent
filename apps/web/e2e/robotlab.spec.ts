import { test, expect } from './fixtures'
import { labSession, mockRobotLab, observationPng, walledObserverScene } from './robotlab-fixture'

// All simulations and agent turns are deterministic local fakes.
test('create an environment in chat, train, focus, observe and replay an old attempt', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  const { bus, calls } = await mockRobotLab(page)
  await page.goto('/lab')
  await expect(page.getByRole('button', { name: 'Room', exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'New', exact: true }).click()
  const composer = page.locator('.lab-agent .composer textarea')
  await expect(composer).toHaveValue('Create a simulation environment from this description: ')
  await composer.fill('Create a simulation environment from this description: a room with a ball')
  await composer.press('Enter')
  await expect(page.getByRole('button', { name: 'New room', exact: true })).toBeVisible()
  expect(
    calls.some(
      (call) =>
        call.method === 'conversation.send' && call.params.conversation_id === 'cnv_web_excalibur',
    ),
  ).toBe(true)
  await page.getByRole('tab', { name: 'Trainings', exact: true }).click()
  await expect(page).toHaveURL(/tab=trainings/)
  await page.getByRole('button', { name: 'New session' }).click()
  await page.getByRole('combobox', { name: 'Robot', exact: true }).selectOption('dog')
  await page.getByRole('combobox', { name: 'Environment', exact: true }).selectOption('room')
  await page.getByRole('textbox', { name: 'Task', exact: true }).fill('Fetch the ball')
  await bus.ready('robotlab.focus')
  await page.getByRole('button', { name: 'Send to agent' }).click()
  await expect(page).toHaveURL(/session=live-training/)
  await bus.ready('robotlab.frame')
  bus.emit('robotlab.frame', {
    session: 'live-training',
    attempt: 2,
    seq: 1,
    t: 0,
    poses: [0, 0, 0, 1, 0, 0, 0, 0, 0, 0.1, 1, 0, 0, 0],
    camera: { name: 'robot/head', pose: [0, -2, 2, 0.9239, 0.3827, 0, 0] },
    held: null,
    status: 'running',
  })
  await expect(page.getByRole('img', { name: 'Observer', exact: true })).toBeVisible()
  await expect(page.getByRole('img', { name: 'Robot camera', exact: true })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Dog · Room' })).toBeVisible()
  await expect(page.locator('.lab-agent .composer textarea')).toBeVisible()
  await expect(page.getByRole('region', { name: 'Latest robot state' })).toContainText(
    'FL · FR · RL',
  )
  await expect(page.getByRole('region', { name: 'Latest robot state' })).toContainText('0.025 m')
  await expect(
    page.getByRole('img', { name: 'Observer', exact: true }).locator('canvas'),
  ).toBeVisible()
  await expect(
    page.getByRole('img', { name: 'Robot camera', exact: true }).locator('canvas'),
  ).toBeVisible()
  const stage = (await page.locator('.lab-stage').boundingBox())!
  const camera = (await page.locator('.lab-camera').boundingBox())!
  expect(camera.width).toBeLessThan(stage.width / 2)
  expect(camera.width / camera.height).toBeCloseTo(4 / 3, 1)
  expect(camera.x).toBeGreaterThan(stage.x)
  expect(camera.y).toBeGreaterThan(stage.y)
  expect(camera.x + camera.width).toBeLessThan(stage.x + stage.width)
  expect(camera.y + camera.height).toBeLessThan(stage.y + stage.height)
  expect(
    calls.some(
      (call) =>
        call.method === 'conversation.send' &&
        call.params.text ===
          'Run a simulation with the Dog in the environment Room: Fetch the ball',
    ),
  ).toBe(true)
  await page.screenshot({
    path: test.info().outputPath('live-session.png'),
    animations: 'disabled',
  })
  await page.getByRole('tab', { name: 'Activity', exact: true }).click()
  await expect(page.getByText('The ball is visible')).toBeVisible()
  const observation = page.getByRole('img', { name: 'Observation', exact: true })
  await expect(observation).toBeVisible()
  await expect(observation).toHaveAttribute('src', `data:image/png;base64,${observationPng}`)
  await expect(observation).toHaveAttribute('loading', 'lazy')
  expect(calls.filter((call) => call.method === 'robotlab.session.image')).toMatchObject([
    {
      method: 'robotlab.session.image',
      params: { session: 'live-training', path: 'attempts/2/looks/1.png' },
    },
  ])
  await page.getByRole('tab', { name: 'Skills', exact: true }).click()
  await expect(page.getByText('Version: 2 · Success: 50% · Uses: 2 · Failures: 1')).toBeVisible()
  await page.getByText('fetch', { exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Lessons' })).toBeVisible()
  await expect(page.getByRole('list', { name: 'Versions' })).toContainText('Version 1')
  await expect(page.getByRole('list', { name: 'Versions' })).toContainText('Version 2')
  await page.getByRole('tab', { name: 'Activity', exact: true }).click()
  await expect(observation).toHaveAttribute('src', `data:image/png;base64,${observationPng}`)
  expect(calls.filter((call) => call.method === 'robotlab.session.image')).toHaveLength(1)
  await page.getByRole('button', { name: 'Back', exact: true }).click()
  await page.getByRole('button', { name: 'Old training' }).click()
  await expect(page).toHaveURL(/session=old-training/)
  await page.getByRole('combobox', { name: 'Attempt', exact: true }).selectOption('1')
  await page.getByRole('button', { name: /Play/ }).click()
  await expect(page.getByRole('button', { name: /Pause/ })).toBeVisible()
  await page.getByRole('button', { name: /Pause/ }).click()
  const timeline = page.getByRole('slider', { name: /Replay time|Replay position/ })
  await timeline.focus()
  await timeline.press('Home')
  await timeline.press('ArrowRight')
  await expect(timeline).toHaveValue('0.05')
})

test('focus navigates from another page and settings save through the lab RPC', async ({
  page,
}) => {
  const { bus, calls } = await mockRobotLab(page)
  await page.goto('/settings?section=robotlab')
  await expect(page.getByLabel('Maximum attempts')).toHaveValue('40')
  await page.getByLabel('Maximum attempts').fill('30')
  await page.getByLabel('Simulation speed').fill('0.5')
  await page.getByRole('button', { name: 'Save', exact: true }).click()
  await expect
    .poll(() =>
      calls.some(
        (call) =>
          call.method === 'robotlab.settings.set' &&
          call.params.max_attempts === 30 &&
          call.params.sim_speed === 0.5,
      ),
    )
    .toBe(true)
  await bus.ready('robotlab.focus')
  bus.emit('robotlab.focus', { session: 'old-training', robot: 'dog', environment: 'room' })
  await expect(page).toHaveURL(/\/lab\?tab=trainings&session=old-training/)
  await expect(page.locator('.lab-agent .composer textarea')).toBeVisible()
})

test('mobile session switches observer, robot camera and agent tabs', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await mockRobotLab(page)
  await page.goto('/lab?tab=trainings&session=old-training')
  await expect(page.getByRole('img', { name: 'Observer', exact: true })).toBeVisible()
  await expect(
    page.getByRole('img', { name: 'Observer', exact: true }).locator('canvas'),
  ).toBeVisible()
  await page.screenshot({
    path: test.info().outputPath('mobile-observer.png'),
    animations: 'disabled',
  })
  await page.getByRole('tab', { name: 'Robot camera', exact: true }).click()
  await expect(page.getByRole('img', { name: 'Robot camera', exact: true })).toBeVisible()
  await expect(page.getByText('Waiting for the robot camera…')).toHaveCount(0)
  await expect(
    page.getByRole('img', { name: 'Robot camera', exact: true }).locator('canvas'),
  ).toBeVisible()
  await page.screenshot({
    path: test.info().outputPath('mobile-camera.png'),
    animations: 'disabled',
  })
  await page
    .getByRole('tablist', { name: 'Robot lab' })
    .getByRole('tab', { name: 'Agent', exact: true })
    .click()
  await expect(page.locator('.lab-mobile-agent .composer textarea')).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
})

test('live scene conflicts recover with loading feedback without reopening the session', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  const { sessions, bus } = await mockRobotLab(page)
  sessions.set('live-training', labSession('live-training', 'live'))
  let sceneCalls = 0
  await page.route('**/rpc', async (route) => {
    const request = route.request().postDataJSON()
    if (request.method !== 'robotlab.session.scene' || ++sceneCalls > 2) return route.fallback()
    await route.fulfill({
      json: {
        jsonrpc: '2.0',
        id: request.id,
        error: { code: -32005, message: 'an action is already running' },
      },
    })
  })
  await page.goto('/lab?tab=trainings&session=live-training')
  await expect(page.getByText('Loading scene…')).toHaveCount(2)
  await expect(page.getByText('The scene could not be loaded.')).toHaveCount(0)
  await bus.ready('robotlab.frame')
  bus.emit('robotlab.frame', {
    session: 'live-training',
    attempt: 2,
    seq: 1,
    t: 0,
    poses: [0, 0, 0, 1, 0, 0, 0, 0, 0, 0.1, 1, 0, 0, 0],
    camera: { name: 'robot/head', pose: [0, -2, 2, 0.9239, 0.3827, 0, 0] },
    held: null,
    status: 'running',
  })
  await expect(page.locator('.lab-observer canvas')).toBeVisible()
  await expect(page.locator('.lab-camera canvas')).toBeVisible()
  expect(sceneCalls).toBe(3)
  await expect(page.getByText('Loading scene…')).toHaveCount(0)
  await expect(page.getByText('Waiting for the robot camera…')).toHaveCount(0)
  await page.getByRole('button', { name: 'Swap views' }).click()
  await expect(page.locator('.lab-camera')).toHaveAttribute('aria-label', 'Observer')
  await expect(
    page.locator('.lab-observer').getByRole('img', { name: 'Robot camera', exact: true }),
  ).toBeVisible()
})

test('installation leads to chat-only environment creation and editing', async ({ page }) => {
  const { calls } = await mockRobotLab(page, false)
  await page.goto('/lab')
  await page.getByRole('button', { name: 'Install robot lab' }).click()
  await page.getByRole('button', { name: 'Room', exact: true }).click()
  await page.getByRole('button', { name: 'Edit with agent' }).click()
  const composer = page.locator('.lab-agent .composer textarea')
  await expect(composer).toHaveValue('Edit environment "Room" (room): ')
  await page.getByRole('button', { name: 'Back', exact: true }).click()
  await page.getByRole('button', { name: 'New', exact: true }).click()
  await expect(composer).toHaveValue('Create a simulation environment from this description: ')
  await expect(page.locator('.lab-workspace input[type=file]')).toHaveCount(0)
  expect(calls.every((call) => !call.method.startsWith('robotlab.media.'))).toBe(true)
})

test('observer frames a dog inside unnamed corridor walls and supports orbit/recenter', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  const { sessions } = await mockRobotLab(page)
  sessions.get('old-training')!.robot = 'dog'
  await page.route('**/rpc', async (route) => {
    const request = route.request().postDataJSON()
    if (request.method === 'robotlab.session.scene') {
      await route.fulfill({
        json: { jsonrpc: '2.0', id: request.id, result: { scene: walledObserverScene() } },
      })
    } else if (request.method === 'robotlab.attempt.frames') {
      await route.fulfill({
        json: {
          jsonrpc: '2.0',
          id: request.id,
          result: {
            total: 1,
            frames: [
              {
                session: 'old-training',
                attempt: 2,
                seq: 0,
                t: 0,
                poses: [0, 0, 0, 1, 0, 0, 0, 0, 0, 0.27, 1, 0, 0, 0],
                camera: { name: 'robot/head', pose: [0.3, 0, 0.35, 0.5, 0.5, -0.5, -0.5] },
                held: null,
                status: 'stopped',
              },
            ],
          },
        },
      })
    } else await route.fallback()
  })
  await page.goto('/lab?tab=trainings&session=old-training')
  await expect(page.getByRole('heading', { name: 'Dog · Room' })).toBeVisible()
  const observer = page.getByRole('img', { name: 'Observer', exact: true })
  await expect(observer.locator('canvas')).toBeVisible()
  await expect(page.getByText('Loading scene…')).toHaveCount(0)
  // Let the first frame reach the GPU before capture.
  await page.evaluate(
    () =>
      new Promise<void>((resolve) =>
        requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
      ),
  )
  await page.screenshot({
    path: test.info().outputPath('dog-observer.png'),
  })
  const canvas = (await observer.locator('canvas').boundingBox())!
  await page.mouse.move(canvas.x + canvas.width / 2, canvas.y + canvas.height / 2)
  await page.mouse.down()
  await page.mouse.move(canvas.x + canvas.width * 0.75, canvas.y + canvas.height * 0.65, {
    steps: 15,
  })
  await page.mouse.up()
  await page.mouse.wheel(0, -120)
  await page.screenshot({
    path: test.info().outputPath('dog-orbit.png'),
  })
  await observer.getByRole('button', { name: 'Recenter' }).click()
  await page.screenshot({
    path: test.info().outputPath('dog-recenter.png'),
  })
  await page.setViewportSize({ width: 390, height: 844 })
  await expect(page.getByRole('button', { name: 'Recenter' })).toBeVisible()
  await page.screenshot({
    path: test.info().outputPath('dog-mobile.png'),
  })
})

test('session chat reveals the latest asynchronous history and allows scrolling back', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  await mockRobotLab(page)
  let releaseHistory: () => void = () => {}
  const historyReady = new Promise<void>((resolve) => {
    releaseHistory = resolve
  })
  await page.route('**/rpc', async (route) => {
    const request = route.request().postDataJSON()
    if (request.method !== 'conversation.history') return route.fallback()
    await historyReady
    await route.fulfill({
      json: {
        jsonrpc: '2.0',
        id: request.id,
        result: {
          messages: Array.from({ length: 80 }, (_, index) => ({
            id: `history-${index}`,
            conversation_id: 'cnv_web_excalibur',
            role: 'assistant',
            text:
              index === 79
                ? 'Latest lab message'
                : `Earlier lab message ${index}. ${'Observation and reflection. '.repeat((index % 5) + 1)}`,
          })),
        },
      },
    })
  })
  await page.goto('/lab?tab=trainings&session=old-training')
  await expect(page.locator('.lab-agent .composer textarea')).toBeVisible()
  releaseHistory()
  await expect(page.getByText('Latest lab message', { exact: true })).toBeInViewport()
  const log = page.locator('.lab-agent .messages')
  await expect
    .poll(() => log.evaluate((el) => el.scrollHeight - el.scrollTop - el.clientHeight))
    .toBeLessThan(5)
  await log.hover()
  await page.mouse.wheel(0, -1200)
  await expect
    .poll(() => log.evaluate((el) => el.scrollHeight - el.scrollTop - el.clientHeight))
    .toBeGreaterThan(400)
})

for (const robot of ['dog', 'arm'] as const) {
  test(`${robot} shows low-level state and the two robot cameras`, async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 })
    const { sessions, calls } = await mockRobotLab(page)
    const session = labSession('camera-training', 'live')
    session.robot = robot
    sessions.set(session.id, session)
    await page.goto('/lab?tab=trainings&session=camera-training')
    const state = page.getByRole('region', { name: 'Latest robot state' })
    await expect(state).toContainText(robot === 'dog' ? 'Support feet' : 'Gripper position')
    await expect(state).toContainText(robot === 'dog' ? '0.025 m' : 'cube')
    const camera = page.getByRole('combobox', { name: 'Camera', exact: true })
    await expect(camera.locator('option')).toHaveText(
      robot === 'dog' ? ['Head', 'Rear'] : ['Wrist', 'Overhead'],
    )
    await camera.selectOption(robot === 'dog' ? 'rear' : 'overhead')
    await expect(page.getByRole('img', { name: 'Robot camera', exact: true })).toHaveAttribute(
      'src',
      `data:image/png;base64,${observationPng}`,
    )
    expect(
      calls.some(
        (call) =>
          call.method === 'robotlab.robot.look' &&
          call.params.camera === (robot === 'dog' ? 'robot/rear' : 'robot/overhead'),
      ),
    ).toBe(true)
    expect(calls.some((call) => call.method === 'robotlab.robot.act')).toBe(false)
  })
}

test('recorded state follows the replay time and attempt', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  await mockRobotLab(page)
  await page.goto('/lab?tab=trainings&session=old-training')
  const state = page.getByRole('region', { name: 'Latest robot state' })
  await expect(state).toContainText('0.025 m')
  const timeline = page.getByRole('slider', { name: /Replay time|Replay position/ })
  await expect(timeline).toHaveValue('10')
  await timeline.focus()
  await timeline.press('Home')
  await expect(state).toContainText('0.040 m')
  for (let step = 0; step < 21; step++) await timeline.press('ArrowRight')
  await expect(state).toContainText('0.025 m')
  await page.getByRole('combobox', { name: 'Attempt', exact: true }).selectOption('1')
  await expect(timeline).toHaveValue('10')
  await timeline.focus()
  await timeline.press('Home')
  await expect(state).toContainText('0.040 m')
})
