import { mkdir } from 'node:fs/promises'
import { test, expect, type Page } from './fixtures'
import { mockEvents } from './event-fixture'
import type { RobotState } from '../src/features/robot/types'

test.use({
  launchOptions: { args: ['--use-fake-device-for-media-stream', '--use-fake-ui-for-media-stream'] },
})

const shots = '/home/cristhian/projects/ui-design/rb-shots'
const initialState: RobotState = {
  connected: false,
  host: '192.168.1.10',
  robot_mode: 'RUNNING',
  safety_mode: 'NORMAL',
  gripper: 'open',
  tcp_pose: [0.32, -0.18, 0.45, 0, 0, 0],
}

async function mockRobot(page: Page) {
  const events = await mockEvents(page)
  const calls: { method: string; params: Record<string, unknown> }[] = []
  const frames: { blob: string; width: number; height: number }[] = []
  const uploads: { mime: string; size: number }[] = []
  let state = { ...initialState }
  let stopError = false
  let connectError = false
  await page.route('**/rpc', async (route) => {
    const request = route.request().postDataJSON()
    if (!request.method.startsWith('robot.')) return route.fallback()
    calls.push(request)
    if (request.method === 'robot.connect' && connectError)
      return route.fulfill({
        json: {
          jsonrpc: '2.0',
          id: request.id,
          error: { code: -32000, message: 'Remote Control required' },
        },
      })
    if (request.method === 'robot.connect')
      state = { ...state, connected: true, host: request.params.host }
    if (request.method === 'robot.disconnect') state = { ...state, connected: false }
    if (request.method === 'robot.frame') frames.push(request.params)
    const result =
      request.method === 'robot.frame'
        ? { ok: true }
        : request.method === 'robot.stop'
          ? { ok: !stopError, ...(stopError ? { error: 'Bridge unavailable' } : {}) }
          : { bridge: 'running', state }
    await route.fulfill({ json: { jsonrpc: '2.0', id: request.id, result } })
  })
  await page.route('**/blobs', async (route) => {
    uploads.push({
      mime: route.request().headers()['content-type'],
      size: route.request().postDataBuffer()?.length ?? 0,
    })
    await route.fulfill({ json: { blob_digest: 'robot-camera-frame' } })
  })
  await page.addInitScript(() => {
    if (!navigator.mediaDevices) return
    const original = navigator.mediaDevices.getUserMedia.bind(navigator.mediaDevices)
    Object.defineProperty(window, '__robotTracksStopped', { value: 0, writable: true })
    navigator.mediaDevices.getUserMedia = async (constraints) => {
      const stream = await original(constraints)
      for (const track of stream.getTracks()) {
        const stop = track.stop.bind(track)
        track.stop = () => {
          Reflect.set(
            window,
            '__robotTracksStopped',
            Number(Reflect.get(window, '__robotTracksStopped')) + 1,
          )
          stop()
        }
      }
      return stream
    }
  })
  return {
    calls,
    frames,
    uploads,
    events,
    emit: (patch: Partial<RobotState>) => {
      state = { ...state, ...patch }
      events.emit('robot.state', { state })
    },
    failStop: () => {
      stopError = true
    },
    failConnect: () => {
      connectError = true
    },
  }
}

test('Robot navigation, remembered IP, connect, live readouts and disconnect in sidebar and rail', async ({
  page,
}) => {
  const robot = await mockRobot(page)
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/projects')
  await page
    .locator('.sidebar')
    .getByRole('button', { name: /^(Robot|Brazo robótico|Robot arm)$/ })
    .click()
  await expect(page).toHaveURL(/\/robot$/)
  await expect(page.getByRole('heading', { level: 1, name: 'Robot', exact: true })).toBeVisible()
  await expect(
    page.locator('.sidebar').getByRole('button', { name: /^(Robot|Brazo robótico|Robot arm)$/ }),
  ).toHaveAttribute('aria-current', 'page')
  await expect(page.locator('.agent-nav button')).not.toHaveAttribute('aria-current', 'page')
  await expect(page.locator('.robot-status')).toHaveText('Desconectado')
  await page.getByRole('textbox', { name: 'Dirección IP del brazo' }).fill('192.168.1.10')
  await page.reload()
  await expect(page.getByRole('textbox', { name: 'Dirección IP del brazo' })).toHaveValue(
    '192.168.1.10',
  )
  await page.getByRole('button', { name: 'Conectar', exact: true }).click()
  await expect(page.locator('.robot-status')).toHaveText('Conectado')
  await expect(page.locator('.robot-readouts')).toContainText('RUNNING')
  await expect(page.locator('.robot-tcp')).toContainText('320.0')
  await robot.events.ready('robot.state')
  robot.emit({
    safety_mode: 'PROTECTIVE_STOP',
    gripper: 'closed',
    tcp_pose: [0.35, -0.2, 0.5, 0, 0, 0],
  })
  await expect(page.locator('.robot-readouts')).toContainText('PROTECTIVE_STOP')
  await expect(page.locator('.robot-readouts')).toContainText('Cerrada')
  await expect(page.locator('.robot-tcp')).toContainText('350.0')
  await page.getByRole('button', { name: 'Desconectar', exact: true }).click()
  await expect(page.locator('.robot-status')).toHaveText('Desconectado')
  await page.keyboard.press('Control+b')
  const rail = page.locator('.sidebar-rail')
  await expect(
    rail.getByRole('button', { name: /^(Robot|Brazo robótico|Robot arm)$/ }),
  ).toHaveAttribute('aria-pressed', 'true')
  await rail.getByRole('button', { name: 'Proyectos', exact: true }).click()
  await rail.getByRole('button', { name: /^(Robot|Brazo robótico|Robot arm)$/ }).click()
  await expect(page).toHaveURL(/\/robot$/)
  expect(
    robot.calls.some(
      (call) => call.method === 'robot.connect' && call.params.host === '192.168.1.10',
    ),
  ).toBe(true)
})

test('camera starts, uploads JPEG frames, changes interval and stops on navigation', async ({
  page,
}) => {
  const robot = await mockRobot(page)
  await page.goto('/robot')
  const picker = page.getByRole('combobox', { name: 'Dispositivo de cámara' })
  await picker.click()
  await expect(page.getByRole('option').last()).toBeVisible()
  await page.getByRole('option').last().click()
  await page.getByRole('button', { name: '1 s', exact: true }).click()
  await page.getByRole('button', { name: 'Iniciar cámara', exact: true }).click()
  await expect(page.getByText('EN DIRECTO', { exact: true })).toBeVisible()
  await expect.poll(() => robot.frames.length).toBeGreaterThanOrEqual(2)
  expect(robot.uploads[0].mime).toBe('image/jpeg')
  expect(robot.uploads[0].size).toBeGreaterThan(0)
  expect(robot.frames[0]).toMatchObject({ blob: 'robot-camera-frame' })
  expect(Math.max(robot.frames[0].width, robot.frames[0].height)).toBeLessThanOrEqual(1280)
  expect(Math.min(robot.frames[0].width, robot.frames[0].height)).toBeGreaterThan(0)
  await expect(page.getByText(/Última foto enviada hace \d+s/)).toBeVisible()
  await page.getByRole('button', { name: '5 s', exact: true }).click()
  await expect(page.getByRole('button', { name: '5 s', exact: true })).toHaveAttribute(
    'aria-pressed',
    'true',
  )
  await page.getByRole('button', { name: 'Detener cámara', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Iniciar cámara', exact: true })).toBeVisible()
  await expect.poll(() => page.evaluate(() => Reflect.get(window, '__robotTracksStopped'))).toBe(1)
  const count = robot.frames.length
  await page.waitForTimeout(1200)
  expect(robot.frames).toHaveLength(count)
  await page.getByRole('button', { name: 'Iniciar cámara', exact: true }).click()
  await expect(page.getByText('EN DIRECTO', { exact: true })).toBeVisible()
  await page.locator('.primary-nav').getByRole('button', { name: 'Proyectos', exact: true }).click()
  await expect.poll(() => page.evaluate(() => Reflect.get(window, '__robotTracksStopped'))).toBe(2)
})

test('STOP stays available disconnected, works by click, Space and Escape, and reports failures', async ({
  page,
}) => {
  const robot = await mockRobot(page)
  await page.goto('/robot')
  const stop = page.getByRole('button', { name: 'STOP', exact: true })
  await expect(stop).toBeInViewport()
  await stop.click()
  await expect(page.getByText('Orden STOP enviada al brazo.', { exact: true })).toBeVisible()
  await page.locator('.robot-page').focus()
  await page.keyboard.press('Space')
  await expect.poll(() => robot.calls.filter((call) => call.method === 'robot.stop').length).toBe(2)
  await page.keyboard.press('Escape')
  await expect.poll(() => robot.calls.filter((call) => call.method === 'robot.stop').length).toBe(3)
  await page.getByRole('textbox', { name: 'Dirección IP del brazo' }).focus()
  await page.keyboard.press('Space')
  expect(robot.calls.filter((call) => call.method === 'robot.stop')).toHaveLength(3)
  robot.failStop()
  await stop.click()
  await expect(page.getByRole('alert')).toContainText('Bridge unavailable')
  await expect(stop).toBeEnabled()
})

test('STOP works while a connection is pending', async ({ page }) => {
  const robot = await mockRobot(page)
  let release!: () => void
  const pending = new Promise<void>((resolve) => {
    release = resolve
  })
  await page.route('**/rpc', async (route) => {
    if (route.request().postDataJSON().method === 'robot.connect') await pending
    return route.fallback()
  })
  await page.goto('/robot')
  await page.getByRole('textbox', { name: 'Dirección IP del brazo' }).fill('192.168.1.10')
  await page.getByRole('button', { name: 'Conectar', exact: true }).click()
  await expect(page.locator('.robot-status')).toHaveText('Conectando…')
  await page.getByRole('button', { name: 'STOP', exact: true }).click()
  await expect.poll(() => robot.calls.filter((call) => call.method === 'robot.stop').length).toBe(1)
  await expect(page.getByText('Orden STOP enviada al brazo.', { exact: true })).toBeVisible()
  release()
  await expect(page.locator('.robot-status')).toHaveText('Conectado')
})

test('camera permissions and connect errors are visible and recoverable', async ({ page }) => {
  const robot = await mockRobot(page)
  robot.failConnect()
  await page.addInitScript(() => {
    navigator.mediaDevices.getUserMedia = async () => {
      throw new DOMException('Denied', 'NotAllowedError')
    }
  })
  await page.goto('/robot')
  await page.getByRole('textbox', { name: 'Dirección IP del brazo' }).fill('192.168.1.10')
  await page.getByRole('button', { name: 'Conectar', exact: true }).click()
  await expect(page.locator('.robot-status')).toContainText('Error · Remote Control required')
  await page.getByRole('button', { name: 'Iniciar cámara', exact: true }).click()
  await expect(page.getByRole('alert')).toContainText('Permite el acceso a la cámara')
  await expect(page.getByRole('button', { name: 'Iniciar cámara', exact: true })).toBeEnabled()
  await expect(page.getByRole('button', { name: 'STOP', exact: true })).toBeEnabled()
})

for (const variant of ['desktop', 'mobile', 'dark', 'english'] as const) {
  test(`Robot visual review ${variant}`, async ({ page }) => {
    const robot = await mockRobot(page)
    const mobile = variant === 'mobile'
    await page.setViewportSize({ width: mobile ? 390 : 1440, height: mobile ? 844 : 900 })
    await page.addInitScript((variant) => {
      localStorage.setItem('excalibur-ui-theme', variant === 'dark' ? 'dark' : 'light')
      localStorage.setItem('excalibur-ui-language', variant === 'english' ? 'en' : 'es')
      localStorage.setItem('world-last-mode', 'world')
    }, variant)
    await page.goto('/robot')
    await expect(page).toHaveURL(/\/robot$/)
    if (variant === 'dark') await expect(page.locator('html')).toHaveClass(/dark/)
    const english = variant === 'english'
    if (mobile) {
      await page.getByRole('button', { name: 'Alternar barra lateral', exact: true }).click()
      await expect(
        page
          .locator('.sidebar')
          .getByRole('button', { name: /^(Robot|Brazo robótico|Robot arm)$/ }),
      ).toBeVisible()
      await page
        .locator('.sidebar')
        .getByRole('button', { name: /^(Robot|Brazo robótico|Robot arm)$/ })
        .click()
      await expect(page.locator('.sidebar')).toBeHidden()
    }
    await page
      .getByRole('textbox', { name: english ? 'Arm IP address' : 'Dirección IP del brazo' })
      .fill('192.168.1.10')
    await page.getByRole('button', { name: english ? 'Connect' : 'Conectar', exact: true }).click()
    await expect(page.locator('.robot-status')).toHaveText(english ? 'Connected' : 'Conectado')
    await robot.events.ready('robot.state')
    if (!mobile) {
      await page
        .getByRole('button', { name: english ? 'Start camera' : 'Iniciar cámara', exact: true })
        .click()
      await expect.poll(() => robot.frames.length).toBeGreaterThan(0)
    }
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    expect(
      await page.locator('.robot-page').evaluate((node) => node.scrollWidth <= node.clientWidth),
    ).toBe(true)
    await expect(page.getByRole('button', { name: 'STOP', exact: true })).toBeInViewport()
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await mkdir(shots, { recursive: true })
    await page.screenshot({ path: `${shots}/rb2-${variant}.png` })
    if (mobile) {
      await page
        .getByRole('button', { name: 'Iniciar cámara', exact: true })
        .scrollIntoViewIfNeeded()
      await page.getByRole('button', { name: 'Iniciar cámara', exact: true }).click()
      await expect.poll(() => robot.frames.length).toBeGreaterThan(0)
      await page
        .locator('.robot-camera-card')
        .evaluate((node) => node.scrollIntoView({ block: 'start' }))
      await expect(page.getByRole('button', { name: 'STOP', exact: true })).toBeInViewport()
      await page.screenshot({ path: `${shots}/rb2-mobile-camera.png` })
    }
  })
}

for (const width of [1440, 390]) {
  test(`Robot shares the Excalibur chat with accessible STOP at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: width === 390 ? 844 : 900 })
    const robot = await mockRobot(page)
    const requests: { method: string; params: Record<string, unknown> }[] = []
    page.on('request', (request) => {
      if (request.url().endsWith('/rpc')) requests.push(request.postDataJSON())
    })
    await page.goto('/robot')
    const connection = page.getByRole('region', { name: 'Conexión del brazo' })
    await expect(connection).toBeVisible()
    await page.getByRole('textbox', { name: 'Dirección IP del brazo' }).fill('192.168.1.10')
    const toggle = page.locator('.robot-header button[aria-controls="robot-conversation"]')
    await expect(toggle).toHaveAccessibleName('Abrir chat')
    await expect(toggle).toHaveAttribute('aria-pressed', 'false')
    await toggle.click()
    await expect(toggle).toHaveAttribute('aria-expanded', 'true')
    await expect(toggle).toHaveAttribute('aria-pressed', 'true')
    const chat = page.getByRole('region', { name: 'Chat de Excalibur' })
    const composer = chat.locator('.composer textarea')
    await expect(composer).toBeInViewport()
    await expect
      .poll(() =>
        requests.some(
          (request) =>
            request.method === 'conversation.history' &&
            request.params.conversation_id === 'cnv_web_excalibur',
        ),
      )
      .toBe(true)
    const stop = page.getByRole('button', { name: 'STOP', exact: true })
    await expect(stop).toBeInViewport()
    await stop.click()
    await expect
      .poll(() => robot.calls.filter((call) => call.method === 'robot.stop').length)
      .toBe(1)
    if (width === 1440) {
      await expect(connection).toBeInViewport()
      const chatBox = await chat.boundingBox()
      const workspaceBox = await page.locator('.robot-workspace').boundingBox()
      expect(chatBox!.x + chatBox!.width).toBeLessThanOrEqual(workspaceBox!.x)
      expect(chatBox!.y).toBe(workspaceBox!.y)
      expect(Math.abs(chatBox!.width - workspaceBox!.width)).toBeLessThan(2)
      const separator = page.getByRole('separator', { name: 'Redimensionar paneles' })
      const handle = await separator.boundingBox()
      await page.mouse.move(handle!.x + handle!.width / 2, handle!.y + handle!.height / 2)
      await page.mouse.down()
      await page.mouse.move(handle!.x + 70, handle!.y + handle!.height / 2)
      await page.mouse.up()
      await expect
        .poll(async () => (await chat.boundingBox())!.width)
        .toBeGreaterThan(chatBox!.width + 40)
      await separator.dblclick()
      await expect
        .poll(async () => (await chat.boundingBox())!.width)
        .toBeCloseTo(chatBox!.width, 0)
      await page.getByRole('button', { name: 'Conectar', exact: true }).click()
      await expect(page.locator('.robot-readouts')).toContainText('RUNNING')
    } else {
      await expect(chat).toHaveClass(/robot-chat-overlay/)
      await expect(page.locator('.robot-workspace')).toHaveAttribute('inert')
      await expect(page.getByRole('separator')).toHaveCount(0)
      const composerBox = await composer.boundingBox()
      const dockBox = await page.locator('.robot-stop-dock').boundingBox()
      expect(composerBox!.y + composerBox!.height).toBeLessThanOrEqual(dockBox!.y)
    }
    await composer.fill('Help me with the robot')
    await composer.press('Enter')
    await expect(chat.locator('.message-user')).toContainText('Help me with the robot')
    await expect
      .poll(() =>
        requests.some(
          (request) =>
            request.method === 'conversation.send' &&
            request.params.conversation_id === 'cnv_web_excalibur' &&
            request.params.text === 'Help me with the robot',
        ),
      )
      .toBe(true)
    // Text input must not trigger the robot's Space shortcut.
    expect(robot.calls.filter((call) => call.method === 'robot.stop')).toHaveLength(1)
    if (width === 390) await chat.getByRole('button', { name: 'Cerrar chat' }).click()
    else await toggle.click()
    await expect(chat).toHaveCount(0)
    await expect(toggle).toHaveAttribute('aria-expanded', 'false')
    await expect(page.locator('.robot-workspace')).not.toHaveAttribute('inert')
    await expect(connection).toBeInViewport()
    await expect(page.getByRole('textbox', { name: 'Dirección IP del brazo' })).toHaveValue(
      '192.168.1.10',
    )
    await expect(stop).toBeInViewport()
    if (width === 1440) {
      await page.goto('/agents/excalibur?view=chat')
      await expect(page.locator('.message-user')).toContainText('Help me with the robot')
      await page.locator('.composer textarea').fill('Reply from the agent page')
      await page.locator('.composer textarea').press('Enter')
      await expect(page.locator('.chat-body')).toContainText('Reply from the agent page')
      await page.goto('/robot')
      await page.getByRole('button', { name: 'Abrir chat', exact: true }).click()
      await expect(chat).toContainText('Reply from the agent page')
    }
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  })
}
