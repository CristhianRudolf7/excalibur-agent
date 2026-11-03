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
  await page.locator('.primary-nav').getByRole('button', { name: 'Robot', exact: true }).click()
  await expect(page).toHaveURL(/\/robot$/)
  await expect(page.getByRole('heading', { level: 1, name: 'Robot', exact: true })).toBeVisible()
  await expect(
    page.locator('.primary-nav').getByRole('button', { name: 'Robot', exact: true }),
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
  await expect(rail.getByRole('button', { name: 'Robot', exact: true })).toHaveAttribute(
    'aria-pressed',
    'true',
  )
  await rail.getByRole('button', { name: 'Proyectos', exact: true }).click()
  await rail.getByRole('button', { name: 'Robot', exact: true }).click()
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
        page.locator('.primary-nav').getByRole('button', { name: 'Robot', exact: true }),
      ).toBeVisible()
      await page.locator('.primary-nav').getByRole('button', { name: 'Robot', exact: true }).click()
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
