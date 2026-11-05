import { mkdir } from 'node:fs/promises'
import { test, expect, type Page } from './fixtures'
import { mockEvents } from './event-fixture'
import type { RobotState } from '../src/features/robot/types'
import type { RobotCalibrationStatus } from '../src/lib/rpc/contract'

test.use({
  launchOptions: { args: ['--use-fake-device-for-media-stream', '--use-fake-ui-for-media-stream'] },
})

const shots = 'test-results/rb-shots'
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
  let tableSet = false
  let calibrationFailure: string | null = null
  let calibration: RobotCalibrationStatus = {
    calibrated: false,
    points: [],
    table_z: 0,
    image: { width: 0, height: 0 },
    reprojection_error_px: 0,
  }
  await page.route('**/rpc', async (route) => {
    const request = route.request().postDataJSON()
    if (!request.method.startsWith('robot.')) return route.fallback()
    calls.push(request)
    if (request.method.startsWith('robot.calibration.')) {
      if (request.method === calibrationFailure) {
        calibrationFailure = null
        return route.fulfill({
          json: {
            jsonrpc: '2.0',
            id: request.id,
            error: { code: -32000, message: 'TCP unavailable; reconnect the arm' },
          },
        })
      }
      if (request.method === 'robot.calibration.add_point') {
        const frame = frames.at(-1)!
        calibration = {
          ...calibration,
          image: { width: frame.width, height: frame.height },
          points: [
            ...calibration.points,
            { pixel: request.params.pixel, base: [state.tcp_pose![0], state.tcp_pose![1]] },
          ],
        }
      }
      if (request.method === 'robot.calibration.set_table_z') {
        tableSet = true
        calibration = { ...calibration, table_z: state.tcp_pose![2] }
      }
      if (request.method === 'robot.calibration.clear') {
        tableSet = false
        calibration = { ...calibration, points: [], table_z: 0, image: { width: 0, height: 0 } }
      }
      calibration = {
        ...calibration,
        calibrated: calibration.points.length >= 4 && tableSet,
        reprojection_error_px: calibration.points.length >= 4 ? 0.24 : 0,
      }
      return route.fulfill({ json: { jsonrpc: '2.0', id: request.id, result: calibration } })
    }
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
    failCalibrationOnce: (method: string) => {
      calibrationFailure = method
    },
  }
}

for (const split of [false, true]) {
  test(`camera calibration captures original frame pixels and clears ${split ? 'with chat' : 'full width'}`, async ({
    page,
  }) => {
    const robot = await mockRobot(page)
    await page.setViewportSize({ width: 1600, height: 1000 })
    await page.goto('/robot')
    const card = page.getByRole('region', { name: 'Calibración de cámara' })
    const mark = card.getByRole('button', { name: 'Marcar punto', exact: true })
    await expect(mark).toBeDisabled()
    await page.getByRole('textbox', { name: 'Dirección IP del brazo' }).fill('192.168.1.10')
    await page.getByRole('button', { name: 'Conectar', exact: true }).click()
    await expect(page.locator('.robot-status')).toHaveText('Conectado')
    if (split) await page.getByRole('button', { name: 'Abrir chat', exact: true }).click()
    await expect(mark).toBeDisabled()
    await page.getByRole('button', { name: 'Iniciar cámara', exact: true }).click()
    await expect.poll(() => robot.frames.length).toBeGreaterThan(0)
    await expect(mark).toBeEnabled()
    await card.getByRole('button', { name: 'Usar altura actual como mesa' }).click()
    await expect(card).toContainText('0.450 m')
    expect(
      robot.calls.find((call) => call.method === 'robot.calibration.set_table_z')?.params,
    ).toEqual({ use_current: true })
    await mark.click()
    const preview = page.getByRole('button', {
      name: 'Añadir punto en la cámara (teclado: centro)',
    })
    await expect(preview).toBeInViewport()
    const frame = robot.frames.at(-1)!
    const marks = [
      [0.25, 0.25],
      [0.75, 0.25],
      [0.75, 0.75],
      [0.25, 0.75],
    ] as const
    for (const [index, [u, v]] of marks.entries()) {
      robot.emit({ tcp_pose: [0.32 + u / 10, -0.18 + v / 10, 0.45, 0, 0, 0] })
      await expect(preview).toBeEnabled()
      const rect = await page.locator('.robot-preview video').boundingBox()
      const scale = Math.min(rect!.width / frame.width, rect!.height / frame.height)
      const left = rect!.x + (rect!.width - frame.width * scale) / 2
      const top = rect!.y + (rect!.height - frame.height * scale) / 2
      // MouseEvent client coordinates are integer CSS pixels in Chromium.
      const x = Math.round(left + u * frame.width * scale)
      const y = Math.round(top + v * frame.height * scale)
      await page.mouse.click(x, y)
      await expect(card).toContainText(`${index + 1} · mínimo 4`)
      const point = robot.calls.filter((call) => call.method === 'robot.calibration.add_point')[
        index
      ]!
      const pixel = point.params.pixel as [number, number]
      expect(pixel[0]).toBeCloseTo((x - left) / scale, 5)
      expect(pixel[1]).toBeCloseTo((y - top) / scale, 5)
      await expect(page.locator('.robot-calibration-markers text')).toHaveCount(index + 1)
    }
    await expect(card.getByText('Calibrada', { exact: true })).toBeVisible()
    await expect(card).toContainText('0.24 px')
    await expect(card).toContainText('(0.345, -0.155) m')
    await page.keyboard.press('Escape')
    await expect(preview).toHaveCount(0)
    await expect(mark).toHaveAttribute('aria-pressed', 'false')
    await card.getByRole('button', { name: 'Borrar calibración', exact: true }).click()
    expect(robot.calls.filter((call) => call.method === 'robot.calibration.clear')).toHaveLength(0)
    await card.getByRole('button', { name: 'Cancelar', exact: true }).click()
    await expect(card.getByText('Calibrada', { exact: true })).toBeVisible()
    await card.getByRole('button', { name: 'Borrar calibración', exact: true }).click()
    await card.getByRole('button', { name: 'Confirmar borrado', exact: true }).click()
    await expect(card.getByText('Sin calibrar', { exact: true })).toBeVisible()
    await expect(card).toContainText('0 · mínimo 4')
    expect(robot.calls.filter((call) => call.method === 'robot.calibration.clear')).toHaveLength(1)
    await expect(page.locator('.robot-calibration-points')).toHaveCount(0)
    await expect(page.getByRole('button', { name: 'STOP', exact: true })).toBeInViewport()
    expect(
      await page
        .locator('.robot-workspace')
        .evaluate((node) => node.scrollWidth <= node.clientWidth),
    ).toBe(true)
  })
}

test('calibration RPC failures remain visible and allow another point', async ({ page }) => {
  const robot = await mockRobot(page)
  await page.goto('/robot')
  await page.getByRole('textbox', { name: 'Dirección IP del brazo' }).fill('192.168.1.10')
  await page.getByRole('button', { name: 'Conectar', exact: true }).click()
  await page.getByRole('button', { name: 'Iniciar cámara', exact: true }).click()
  await expect.poll(() => robot.frames.length).toBeGreaterThan(0)
  await page.getByRole('button', { name: 'Marcar punto', exact: true }).click()
  robot.failCalibrationOnce('robot.calibration.add_point')
  const preview = page.getByRole('button', { name: 'Añadir punto en la cámara (teclado: centro)' })
  await preview.click()
  await expect(page.getByRole('alert')).toContainText('TCP unavailable')
  await expect(page.getByRole('button', { name: 'STOP', exact: true })).toBeEnabled()
  await page.getByRole('button', { name: 'Actualizar estado', exact: true }).click()
  await expect(page.getByRole('alert')).toHaveCount(0)
  await preview.click()
  await expect(page.getByRole('region', { name: 'Calibración de cámara' })).toContainText(
    '1 · mínimo 4',
  )
})

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
