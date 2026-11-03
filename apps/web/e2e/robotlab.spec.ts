import { readFileSync } from 'node:fs'
import { test, expect, type Page } from './fixtures'
import { mockEvents } from './event-fixture'
import type { Experiment, ScenarioInfo, Scene } from '../src/features/robotlab/types'
const scenarios: ScenarioInfo[] = [
  'cartpole',
  'arm',
  'ballplate',
  'quadruped',
  'drone',
  'rocket',
].map((id) => ({
  id,
  title: { en: id, es: id },
  description: { en: 'A physical world', es: 'A physical world' },
  category: 'classic',
  params_schema: { properties: { links: { type: 'integer', minimum: 1, maximum: 3 } } },
  default_params: { links: 1 },
  actuators: [
    {
      name: 'motor',
      label: { en: 'Motor', es: 'Motor' },
      kind: 'torque',
      range: [-10, 10],
      key_neg: 'KeyA',
      key_pos: 'KeyD',
    },
  ],
  reward_terms: [{ name: 'balance', weight: 1, description: { en: 'Balance', es: 'Balance' } }],
  algorithms: ['PPO', 'SAC'],
  default_total_steps: 100,
  inputs: ['sliders', 'keyboard', 'gamepad', 'pose'],
  has_pretrained: true,
  has_vision: id === 'arm',
  thumbnail_color: '#2F6BFF',
}))
const renderFixtures: Record<string, { scene: Scene; frame: Record<string, unknown> }> = process.env
  .EXCALIBUR_ROBOTLAB_SCENE_FIXTURES
  ? JSON.parse(readFileSync(process.env.EXCALIBUR_ROBOTLAB_SCENE_FIXTURES, 'utf8'))
  : {}
const scene = (scenario: string): Scene => ({
  scenario,
  params: { links: 1 },
  timestep: 0.02,
  bodies: [{ name: 'world', parent: -1 }],
  geoms: [
    {
      body: 0,
      type: 'box',
      size: [0.1, 0.1, 0.1],
      pos: [0, 0, 0.1],
      quat: [1, 0, 0, 0],
      rgba: [0.2, 0.4, 0.9, 1],
    },
  ],
  cameras: [],
  actuators: scenarios[0]!.actuators,
  camera_default: { pos: [3, -3, 2], target: [0, 0, 0] },
})
async function mockLab(page: Page, installedInitially = true, useFixtures = false) {
  const bus = await mockEvents(page)
  let installed = installedInitially
  let experiment: Experiment | undefined
  let mapping = { source: 'default', code: 'def map_pose(pose, state):\n    return {}' }
  let count = 0
  const calls: { method: string; params: Record<string, unknown> }[] = []
  const sessions = new Map<string, string>([['agent-arm', 'arm']])
  await page.route('**/rpc', async (route) => {
    const request = route.request().postDataJSON()
    if (!request.method.startsWith('robotlab.')) return route.fallback()
    calls.push(request)
    let result: unknown = {}
    switch (request.method) {
      case 'robotlab.status':
        result = {
          enabled: true,
          installed,
          python: 'python',
          running: installed,
          requirements: { python: '>=3.10', osmesa: true },
        }
        break
      case 'robotlab.install':
        installed = true
        result = { started: true }
        break
      case 'robotlab.scenarios':
        result = { scenarios }
        break
      case 'robotlab.session.open': {
        const session = `session-${++count}`
        sessions.set(session, request.params.scenario)
        result = {
          session,
          scene:
            (useFixtures ? renderFixtures[request.params.scenario]?.scene : undefined) ??
            scene(request.params.scenario),
        }
        break
      }
      case 'robotlab.session.reset':
        result = { scene: scene(sessions.get(request.params.session) ?? 'cartpole') }
        break
      case 'robotlab.teleop.get':
        result = mapping
        break
      case 'robotlab.teleop.mapping':
        mapping =
          request.params.code == null
            ? { source: 'default', code: 'def map_pose(pose, state):\n    return {}' }
            : { source: 'custom', code: request.params.code }
        result = { ok: true }
        break
      case 'robotlab.session.control':
        result = { mode: request.params.mode }
        break
      case 'robotlab.experiment.list':
        result = { experiments: experiment ? [experiment] : [] }
        break
      case 'robotlab.experiment.create':
        experiment = {
          ...request.params,
          id: 'exp-1',
          status: 'draft',
          created_at: '2026-11-02',
          code: 'print("hello")',
          runs: request.params.runs.map((run: Record<string, unknown>, i: number) => ({
            ...run,
            id: `run-${i}`,
            status: 'draft',
            steps: 0,
            wall_s: 0,
            checkpoints: [],
          })),
        }
        result = experiment
        break
      case 'robotlab.experiment.code':
        if (request.params.code.includes('invalid python'))
          result = { ok: false, errors: ['Syntax error in experiment.py'] }
        else {
          experiment = { ...experiment!, code: request.params.code }
          result = { ok: true }
        }
        break
      case 'robotlab.session.policy':
        result = { mode: 'policy' }
        break
      case 'robotlab.experiment.get':
        result = experiment
        break
      case 'robotlab.experiment.metrics':
        result = { points: [], next: 0 }
        break
      case 'robotlab.experiment.start':
        experiment = { ...experiment!, status: 'running' }
        result = experiment
        break
      case 'robotlab.experiment.stop':
        experiment = { ...experiment!, status: 'stopped' }
        result = experiment
        break
    }
    await route.fulfill({ json: { jsonrpc: '2.0', id: request.id, result } })
  })
  return {
    bus,
    calls,
    getExperiment: () => experiment!,
    getSession: () => `session-${count}`,
    publishExperiment: (patch: Partial<Experiment>) => {
      experiment = { ...experiment!, ...patch }
      bus.emit('robotlab.experiment', experiment)
    },
  }
}
for (const mobile of [false, true]) {
  test(`tool store installs and opens the lab ${mobile ? 'on mobile' : 'on desktop'}`, async ({
    page,
  }) => {
    await page.setViewportSize(mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 })
    const { bus, calls } = await mockLab(page, false)
    await page.goto('/coming-soon/tools')
    await bus.ready('robotlab.install')
    await page.getByRole('button', { name: /^Install$|^Instalar$/ }).click()
    await expect.poll(() => calls.some((call) => call.method === 'robotlab.install')).toBe(true)
    bus.emit('robotlab.install', { phase: 'packages', message: 'Installing packages', percent: 50 })
    await expect(page.getByRole('progressbar')).toHaveAttribute('value', '50')
    bus.emit('robotlab.install', { phase: 'done', message: 'Ready', percent: 100, done: true })
    await page.getByRole('link', { name: /Open Robot Lab|Abrir laboratorio/ }).click()
    await expect(page).toHaveURL(/\/lab$/)
    await expect(page.getByRole('tab', { name: /Scene|Escena/ })).toBeVisible()
    if (mobile)
      await page
        .getByRole('button', { name: /^Controls$|^Controles$/ })
        .first()
        .click()
    const slider = page.getByRole('slider', { name: 'Motor' })
    await expect(slider).toBeVisible()
    await slider.evaluate((node) => {
      const input = node as HTMLInputElement
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, '0.5')
      input.dispatchEvent(new Event('input', { bubbles: true }))
      input.dispatchEvent(new Event('change', { bubbles: true }))
    })
    await expect
      .poll(() =>
        calls.some(
          (call) =>
            call.method === 'robotlab.session.control' &&
            (call.params.inputs as Record<string, number> | undefined)?.motor === 0.5,
        ),
      )
      .toBe(true)
  })
}
test('experiment creation, live curves and agent focus switching', async ({ page }) => {
  const { bus, calls } = await mockLab(page)
  await page.goto('/lab')
  await expect(page.getByRole('slider', { name: 'Motor' })).toBeVisible()
  await page.getByRole('tab', { name: /Experiments|Experimentos/ }).click()
  await page.getByRole('button', { name: /New experiment|Nuevo experimento/ }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByRole('textbox', { name: /^Name$|^Nombre$/ }).fill('Balance comparison')
  await dialog.getByRole('button', { name: /Add run|Añadir ejecución/ }).click()
  await dialog.getByRole('button', { name: /Create experiment|Crear experimento/ }).click()
  await expect(page.getByRole('heading', { name: 'Balance comparison' })).toBeVisible()
  await page.getByRole('button', { name: /Start training|Iniciar entrenamiento/ }).click()
  await expect
    .poll(() => calls.some((call) => call.method === 'robotlab.experiment.start'))
    .toBe(true)
  await bus.ready('robotlab.metrics')
  bus.emit('robotlab.metrics', {
    experiment: 'exp-1',
    points: [
      { run: 'run-0', step: 1, wall_s: 1, episode_return: 2 },
      { run: 'run-0', step: 2, wall_s: 2, episode_return: 5 },
      { run: 'run-1', step: 1, wall_s: 1, episode_return: 3 },
    ],
  })
  await expect(page.getByTestId('lab-reward-chart')).toHaveAttribute('data-points', '3')
  await expect(page.getByTestId('lab-reward-chart').locator('svg')).toBeVisible()
  bus.emit('robotlab.focus', { session: 'agent-arm', view: 'vision' })
  await Promise.all([
    expect(page.getByRole('tab', { name: /Vision|Visión/ })).toHaveAttribute(
      'aria-selected',
      'true',
    ),
    expect(page.getByRole('status').filter({ hasText: /agent opened|agente abrió/ })).toBeVisible(),
  ])
})
test('agent focus outside the lab offers a chat-side link', async ({ page }) => {
  const { bus } = await mockLab(page)
  await page.goto('/agents/excalibur?view=chat')
  await bus.ready('robotlab.focus')
  bus.emit('robotlab.focus', { session: 'agent-arm', view: 'scene' })
  await expect(page.getByRole('link', { name: /Open Robot Lab|Abrir laboratorio/ })).toBeVisible()
  await page.getByRole('link', { name: /Open Robot Lab|Abrir laboratorio/ }).click()
  await expect(page.getByRole('slider', { name: 'Motor' })).toBeVisible()
})

test('experiment Python, logs, stopping and best-policy playback', async ({ page }) => {
  const { bus, calls, getExperiment, publishExperiment } = await mockLab(page)
  await page.goto('/lab')
  await expect(page.getByRole('slider', { name: 'Motor' })).toBeVisible()
  await page.getByRole('tab', { name: /Experiments|Experimentos/ }).click()
  await page.getByRole('button', { name: /New experiment|Nuevo experimento/ }).click()
  await page
    .getByRole('dialog')
    .getByRole('textbox', { name: /^Name$|^Nombre$/ })
    .fill('Policy workshop')
  await page.getByRole('button', { name: /Create experiment|Crear experimento/ }).click()
  await page.getByRole('button', { name: /Start training|Iniciar entrenamiento/ }).click()
  await page.getByRole('button', { name: /^Code$|^Código$/ }).click()
  const editor = page.getByRole('code')
  await expect(editor).toBeVisible()
  await editor.click()
  await page.keyboard.press('ControlOrMeta+a')
  await page.keyboard.insertText('invalid python')
  await page.getByRole('button', { name: /Save code|Guardar código/ }).click()
  await expect(
    page.getByRole('alert').filter({ hasText: 'Syntax error in experiment.py' }),
  ).toHaveText('Syntax error in experiment.py')
  await editor.click()
  await page.keyboard.press('ControlOrMeta+a')
  await page.keyboard.insertText('print("policy workshop")')
  await page.getByRole('button', { name: /Save code|Guardar código/ }).click()
  await expect(
    page.getByRole('status').filter({ hasText: /Code saved|Código guardado/ }),
  ).toBeVisible()
  expect(getExperiment().code).toContain('policy workshop')
  await page.getByRole('button', { name: /^Logs$|^Registros$/ }).click()
  bus.emit('robotlab.log', { source: 'runtime', line: 'Evaluation complete: return 5' })
  await expect(page.getByRole('log')).toContainText('Evaluation complete: return 5')
  publishExperiment({
    runs: getExperiment().runs.map((run) => ({
      ...run,
      checkpoints: ['best' as const],
      best_return: 5,
    })),
  })
  await page.getByRole('button', { name: /Stop training|Detener entrenamiento/ }).click()
  await expect
    .poll(() => calls.some((call) => call.method === 'robotlab.experiment.stop'))
    .toBe(true)
  await page.getByRole('button', { name: /^Overview$|^Resumen$/ }).click()
  await page
    .getByRole('button', { name: /Play best policy|Reproducir mejor política/ })
    .first()
    .click()
  await expect(page.getByRole('tab', { name: /Scene|Escena/ })).toHaveAttribute(
    'aria-selected',
    'true',
  )
  await expect
    .poll(() => calls.some((call) => call.method === 'robotlab.session.policy'))
    .toBe(true)
  const session = calls.find((call) => call.method === 'robotlab.session.policy')!.params.session
  bus.emit('robotlab.frame', {
    session,
    t: 1,
    step: 42,
    episode: 2,
    bodies: [[0, 0, 0, 1, 0, 0, 0]],
    reward: 1,
    episode_return: 5,
    terminated: false,
    info: {},
    mode: 'policy',
  })
  await expect(page.locator('.lab-hud').getByText('42', { exact: true })).toBeVisible()
  await page.locator('.lab-chat-toggle').click()
  await expect(page.locator('.lab-live-hud')).toContainText(/42.*5\.0/)
  await expect(page.locator('.lab-live-hud')).toBeVisible()
})

async function mockCamera(page: Page, deny = false) {
  await page.addInitScript(
    ({ deny }) => {
      Object.defineProperty(navigator, 'mediaDevices', {
        value: {
          getUserMedia: async () => {
            if (deny) throw new DOMException('Camera permission denied', 'NotAllowedError')
            const canvas = document.createElement('canvas')
            canvas.width = 640
            canvas.height = 480
            const context = canvas.getContext('2d')!
            const timer = setInterval(() => {
              context.fillStyle = '#ccc'
              context.fillRect(0, 0, 640, 480)
            }, 33)
            const stream = canvas.captureStream(30)
            stream.getTracks()[0]!.addEventListener('ended', () => clearInterval(timer))
            return stream
          },
          enumerateDevices: async () => [
            { kind: 'videoinput', deviceId: 'test-camera', label: 'Test camera' },
          ],
        },
      })
    },
    { deny },
  )
  // The build-time tracker fixture ignores model bytes; all requests stay local.
  await page.route('**/*.task', (route) => route.fulfill({ body: 'fixture' }))
}

test('lab teleop editor reads the current mapping and refreshes after save/reset', async ({
  page,
}) => {
  const { calls } = await mockLab(page)
  await page.goto('/lab')
  await page.getByRole('tab', { name: /Teleoperation|Teleoperación/ }).click()
  await expect(page.getByRole('img', { name: 'cartpole 3D' })).toBeVisible()
  await page.getByRole('button', { name: /View\/edit|Ver\/editar/ }).click()
  const editor = page.getByRole('code')
  await expect(editor).toContainText('def map_pose')
  expect(calls.find((call) => call.method === 'robotlab.teleop.get')?.params).toEqual({
    session: 'session-1',
  })
  await editor.click()
  await page.keyboard.press('ControlOrMeta+a')
  await page.keyboard.insertText('def map_pose(pose, state):\n    return {"motor": 0.5}')
  await page.getByRole('button', { name: /Save mapping|Guardar mapeo/ }).click()
  await expect(page.getByText(/Agent\/user mapping|Mapeo del agente/)).toBeVisible()
  await expect(editor).toContainText('motor')
  await page.getByRole('button', { name: /Use scenario default|Usar predeterminado/ }).click()
  await expect(page.getByText(/Scenario default mapping|Mapeo predeterminado/)).toBeVisible()
  await expect(editor).toContainText('return {}')
  expect(calls.filter((call) => call.method === 'robotlab.teleop.get')).toHaveLength(3)
})

test('lab camera tracking, calibration, hold and stop send pose then idle', async ({ page }) => {
  await mockCamera(page)
  const { calls } = await mockLab(page)
  await page.goto('/lab')
  await page.getByRole('tab', { name: /Teleoperation|Teleoperación/ }).click()
  await page.getByRole('button', { name: /Enable camera|Activar cámara/ }).click()
  await expect(page.getByRole('status').filter({ hasText: /Tracking|Seguimiento/ })).toBeVisible()
  const controls = () => calls.filter((call) => call.method === 'robotlab.session.control')
  expect(controls().filter((call) => call.params.mode === 'pose')).toHaveLength(0)
  await page.getByRole('button', { name: /Calibrate neutral|Calibrar postura/ }).click()
  await expect(page.getByText(/Neutral pose captured|Postura neutra capturada/)).toBeVisible({
    timeout: 7000,
  })
  await page.locator('h1').click()
  await page.keyboard.down('Space')
  await expect
    .poll(() => controls().filter((call) => call.params.mode === 'pose').length)
    .toBeGreaterThan(1)
  const pose = controls().find((call) => call.params.mode === 'pose')!.params.pose as {
    landmarks: unknown[]
    hands: unknown[]
    calibration: unknown
  }
  expect(pose.landmarks).toHaveLength(33)
  expect(pose.hands).toHaveLength(1)
  expect(pose.calibration).toBeDefined()
  await page.keyboard.up('Space')
  await expect.poll(() => controls().at(-1)?.params.mode).toBe('idle')
  await page.getByRole('button', { name: /Turn camera off|Apagar cámara/ }).click()
  await expect(page.getByRole('button', { name: /Enable camera|Activar cámara/ })).toBeEnabled()
  expect(controls().at(-1)?.params.mode).toBe('idle')
})

test('lab camera permission denial offers retry', async ({ page }) => {
  await mockCamera(page, true)
  await mockLab(page)
  await page.goto('/lab')
  await page.getByRole('tab', { name: /Teleoperation|Teleoperación/ }).click()
  await page.getByRole('button', { name: /Enable camera|Activar cámara/ }).click()
  await expect(page.getByRole('alert')).toContainText('Camera permission denied')
  await expect(page.getByRole('button', { name: /Enable camera|Activar cámara/ })).toBeEnabled()
})

test('lab vision shows streamed phases, selected marks and verification', async ({ page }) => {
  const { bus, calls } = await mockLab(page)
  await page.goto('/lab')
  await bus.ready('robotlab.focus')
  bus.emit('robotlab.focus', { session: 'agent-arm', view: 'vision' })
  const panel = page.getByRole('region', { name: /What the agent sees|Lo que ve el agente/ })
  await panel.getByRole('textbox').fill('Put the blue cube in the tray')
  await panel.getByRole('button', { name: /Run step|Ejecutar paso/ }).click()
  await expect
    .poll(() => calls.find((call) => call.method === 'robotlab.vision.step')?.params)
    .toEqual({ session: 'agent-arm', instruction: 'Put the blue cube in the tray' })
  const image = await page.evaluate(() => {
    const canvas = document.createElement('canvas')
    canvas.width = 640
    canvas.height = 480
    return canvas.toDataURL().split(',')[1]
  })
  const phases = {
    see: /See|Ver/,
    think: /Think|Pensar/,
    act: /Act|Actuar/,
    verify: /Verify|Verificar/,
  }
  for (const [phase, label] of Object.entries(phases)) {
    bus.emit('robotlab.vision', {
      session: 'agent-arm',
      step_id: 'step-one',
      phase,
      image_png_b64: image,
      marks: [{ id: 1, bbox: [64, 48, 320, 240] }],
      decision: {
        objects: [{ mark: 1, class: 'cube', reason: 'It is blue' }],
        action: { mark: 1, target: 'tray' },
        explanation: 'Sort by color',
        ...(phase === 'verify'
          ? { verified: true, model_verified: false, verify_note: 'simulation' }
          : {}),
      },
      model: 'fake-vision',
      latency_ms: 40,
    })
    await expect(panel.locator('[aria-current=step]')).toHaveText(label)
  }
  await expect(panel.getByText('Sort by color')).toBeVisible()
  await expect(panel.getByText(/Verified by simulation|Verificado por simulación/)).toContainText(
    '✓',
  )
  await expect(panel.getByText(/Model opinion|Opinión del modelo/)).toContainText('✗')
  await expect(panel.locator('rect.robot-mark-selected')).toHaveCount(1)
})

test('scenario thumbnails have inspection scale and labels', async ({ page }) => {
  await mockLab(page)
  await page.goto('/coming-soon/tools')
  const thumbnails = page.locator('.lab-thumbnails svg')
  await expect(thumbnails).toHaveCount(6)
  for (const thumbnail of await thumbnails.all()) {
    const box = await thumbnail.boundingBox()
    expect(box!.width).toBeGreaterThanOrEqual(96)
    expect(box!.height).toBeGreaterThanOrEqual(80)
  }
  await expect(page.locator('.lab-scenario-tile figcaption')).toHaveCount(6)
  await page.screenshot({
    animations: 'disabled',
    path: test.info().outputPath('store-desktop.png'),
  })
  await page.setViewportSize({ width: 390, height: 844 })
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await page.screenshot({
    animations: 'disabled',
    path: test.info().outputPath('store-mobile.png'),
  })
})

for (const id of ['cartpole', 'arm', 'ballplate', 'quadruped', 'drone', 'rocket']) {
  test(`scenario framing and desktop controls: ${id}`, async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 1000 })
    const { bus, getSession } = await mockLab(page, true, true)
    await page.goto('/lab')
    await expect(page.getByRole('img', { name: 'cartpole 3D' })).toBeVisible()
    if (id !== 'cartpole') {
      await page.getByRole('combobox', { name: /Scenario|Escenario/ }).click()
      await page.getByRole('option', { name: id, exact: true }).click()
    }
    await expect(page.getByRole('img', { name: `${id} 3D` })).toBeVisible()
    await expect(page.locator('.lab-controls-close')).not.toBeVisible()
    await bus.ready('robotlab.frame')
    const session = getSession()
    bus.emit('robotlab.frame', {
      session,
      t: 0,
      step: 0,
      episode: 0,
      bodies: [[0, 0, 0, 1, 0, 0, 0]],
      reward: 0,
      episode_return: 0,
      terminated: false,
      info: {},
      mode: 'idle',
      ...renderFixtures[id]?.frame,
      // Snapshot exports use a placeholder session ID.
      ...{ session, step: 7 },
    })
    await expect(page.getByRole('img', { name: `${id} 3D` }).locator('canvas')).toBeVisible()
    await expect(page.locator('.lab-run-overlay [role=status]')).toBeVisible()
    await expect(page.locator('.lab-hud strong').first()).toHaveText('7')
    // Allow the next animation frame to apply the pose before capturing WebGL.
    await page.evaluate(
      () => new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve))),
    )
    await page.screenshot({
      animations: 'disabled',
      path: test.info().outputPath(`${id}-desktop.png`),
    })
  })
}

for (const viewport of [
  { width: 1440, height: 900 },
  { width: 1024, height: 768 },
  { width: 390, height: 844 },
]) {
  test(`lab layout and shared chat at ${viewport.width}`, async ({ page }) => {
    await page.setViewportSize(viewport)
    const requests: { method: string; params: Record<string, unknown> }[] = []
    page.on('request', (request) => {
      if (request.url().endsWith('/rpc')) requests.push(request.postDataJSON())
    })
    await mockLab(page)
    await page.goto('/lab')
    await expect(page.locator('.lab-viewer canvas')).toBeVisible()
    await expect(page.locator('.page-sidebar-toggle')).toHaveCount(0)
    await expect(page.locator('.lab-header > .panel-toggle')).toBeVisible()
    await expect(page.locator('.lab-header p')).toHaveCount(0)
    const title = await page.locator('.lab-header h1').boundingBox()
    const toggle = await page.locator('.lab-header > .panel-toggle').boundingBox()
    expect(toggle!.x + toggle!.width).toBeLessThanOrEqual(title!.x)
    if (viewport.width === 390) {
      await page.locator('.lab-header > .panel-toggle').click()
      await page.screenshot({
        animations: 'disabled',
        path: `/home/cristhian/excalibur-sim/scratch/lab-layout/390-drawer.png`,
      })
      await page.locator('.lab-nav button').click()
    }
    await expect(page.locator('.lab-nav button')).toHaveAttribute('aria-current', 'page')
    const switcher = page.getByRole('combobox', { name: /Scenario|Escenario/ })
    const box = await switcher.boundingBox()
    expect(box!.width).toBeGreaterThan(120)
    expect(box!.x + box!.width).toBeLessThanOrEqual(viewport.width)
    await switcher.click()
    await page.screenshot({
      animations: 'disabled',
      path: `/home/cristhian/excalibur-sim/scratch/lab-layout/${viewport.width}-scenarios.png`,
    })
    await page.getByRole('option', { name: 'arm', exact: true }).click()
    await expect(page.getByRole('img', { name: 'arm 3D' })).toBeVisible()
    await page.screenshot({
      animations: 'disabled',
      path: `/home/cristhian/excalibur-sim/scratch/lab-layout/${viewport.width}-scene.png`,
    })
    await page.locator('.lab-chat-toggle').click()
    await expect(page.locator('.lab-chat .composer textarea')).toBeVisible()
    await expect
      .poll(() =>
        requests.some(
          (r) =>
            r.method === 'conversation.history' && r.params.conversation_id === 'cnv_web_excalibur',
        ),
      )
      .toBe(true)
    expect(requests.some((r) => r.params?.conversation_id === 'cnv_web_robotlab')).toBe(false)
    if (viewport.width === 390) {
      await expect(page.locator('.lab-chat-overlay')).toBeVisible()
      await expect(page.locator('.lab-workspace')).toHaveAttribute('inert', '')
      await page.screenshot({
        animations: 'disabled',
        path: `/home/cristhian/excalibur-sim/scratch/lab-layout/${viewport.width}-chat.png`,
      })
      await page.getByRole('button', { name: /Back to Robot Lab|Volver al laboratorio/ }).click()
      await expect(page.locator('.lab-workspace')).not.toHaveAttribute('inert')
    } else {
      const chat = await page.locator('.lab-chat').boundingBox()
      const lab = await page.locator('.lab-workspace').boundingBox()
      expect(chat!.x + chat!.width).toBeLessThanOrEqual(lab!.x)
      expect(chat!.y).toBe(lab!.y)
      await expect(page.locator('.lab-side')).not.toBeVisible()
      await page.screenshot({
        animations: 'disabled',
        path: `/home/cristhian/excalibur-sim/scratch/lab-layout/${viewport.width}-split.png`,
      })
      await page.getByRole('button', { name: /^(Controls|Controles)$/ }).click()
      await expect(page.getByRole('dialog', { name: /Controls|Controles/ })).toHaveAttribute(
        'aria-modal',
        'false',
      )
      expect(await page.locator('[inert], .lab-sheet-backdrop').count()).toBe(0)
      await expect(
        page.getByRole('button', { name: /Close controls|Cerrar controles/ }),
      ).toBeVisible()
      await page.locator('.lab-viewer canvas').click({ position: { x: 5, y: 5 } })
      await expect(page.locator('.lab-side-open')).toBeVisible()
      const panel = await page.locator('.lab-side-open').boundingBox()
      expect(panel!.x).toBeGreaterThanOrEqual(lab!.x)
      await page.locator('.lab-chat .composer textarea').fill('Shared lab message')
      await page.screenshot({
        animations: 'disabled',
        path: `/home/cristhian/excalibur-sim/scratch/lab-layout/${viewport.width}-chat.png`,
      })
      await page.keyboard.press('Escape')
      await expect(page.locator('.lab-side')).not.toBeVisible()
      await page.locator('.lab-chat .composer textarea').press('Enter')
      await expect
        .poll(() =>
          requests.some(
            (r) =>
              r.method === 'conversation.send' && r.params.conversation_id === 'cnv_web_excalibur',
          ),
        )
        .toBe(true)
      await page.goto('/agents/excalibur?view=chat')
      await expect(page.locator('.chat-body')).toContainText('Shared lab message')
      await page.locator('.composer textarea').fill('Reply from agent page')
      await page.locator('.composer textarea').press('Enter')
      await expect(page.locator('.chat-body')).toContainText('Reply from agent page')
      await page.goto('/lab')
      await page.locator('.lab-chat-toggle').click()
      await expect(page.locator('.lab-chat')).toContainText('Reply from agent page')
      await page.getByRole('button', { name: /^(Controls|Controles)$/ }).click()
      await expect(page.locator('.lab-side-open')).toBeVisible()
      await page.locator('.lab-chat-toggle').click()
      await expect(page.locator('.lab-side')).toBeVisible()
      await expect(page.locator('.lab-side')).not.toHaveClass(/lab-side-open/)
      const stage = await page.locator('.lab-stage').boundingBox()
      const controls = await page.locator('.lab-side').boundingBox()
      expect(stage!.x + stage!.width).toBeLessThanOrEqual(controls!.x)
    }
  })
}

test('lab split preference persists and collapsed rail routes back to the lab', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  await mockLab(page)
  await page.goto('/lab')
  await page.locator('.lab-chat-toggle').click()
  const separator = page.getByRole('separator', { name: /Resize panels|Redimensionar paneles/ })
  await expect(separator).toBeVisible()
  const before = Number(await separator.getAttribute('aria-valuenow'))
  await separator.focus()
  await page.keyboard.press('ArrowLeft')
  await expect
    .poll(async () => Number(await separator.getAttribute('aria-valuenow')))
    .not.toBe(before)
  const ratio = await page.evaluate(() => localStorage.getItem('chat-split-ratio-v2'))
  await page.reload()
  await page.locator('.lab-chat-toggle').click()
  expect(await page.evaluate(() => localStorage.getItem('chat-split-ratio-v2'))).toBe(ratio)
  await page.locator('.lab-header > .panel-toggle').click()
  const rail = page.locator('.sidebar-rail')
  await expect(
    rail.getByRole('button', { name: /Robot Lab|Laboratorio robótico/ }),
  ).toHaveAttribute('aria-current', 'page')
  await page.screenshot({
    animations: 'disabled',
    path: '/home/cristhian/excalibur-sim/scratch/lab-layout/1440-rail.png',
  })
  await rail.getByRole('button', { name: /Tools|Herramientas/, exact: true }).click()
  await rail.getByRole('button', { name: /Robot Lab|Laboratorio robótico/ }).click()
  await expect(page).toHaveURL(/\/lab$/)
})

for (const viewport of [
  { width: 1440, height: 900 },
  { width: 390, height: 844 },
]) {
  test(`pause state and frozen steps survive chat changes at ${viewport.width}`, async ({
    page,
  }) => {
    await page.setViewportSize(viewport)
    const { bus, calls, getSession } = await mockLab(page)
    await page.goto('/lab')
    await expect(page.locator('.lab-viewer canvas')).toBeVisible()
    await bus.ready('robotlab.frame')
    let step = 0
    const timer = setInterval(() => {
      const mode =
        calls
          .filter(
            (call) =>
              call.method === 'robotlab.session.control' && call.params.session === getSession(),
          )
          .at(-1)?.params.mode ?? 'idle'
      if (mode === 'paused') return
      bus.emit('robotlab.frame', {
        session: getSession(),
        t: step / 30,
        step: ++step,
        episode: 0,
        bodies: [[0, 0, 0, 1, 0, 0, 0]],
        reward: 0,
        episode_return: 0,
        terminated: false,
        info: {},
        mode,
      })
    }, 100)
    try {
      const host = page.locator(viewport.width === 1440 ? '.lab-controls' : '.lab-live-hud')
      const counter =
        viewport.width === 1440
          ? page.locator('.lab-hud strong').first()
          : page.locator('.lab-live-hud > span strong').first()
      await expect.poll(async () => Number(await counter.textContent())).toBeGreaterThan(1)
      await host.getByRole('button', { name: /Pause simulation|Pausar simulación/ }).click()
      await expect(host.locator('.lab-run-status')).toHaveText(/Paused|En pausa/)
      let frozen = await counter.textContent()
      await page.waitForTimeout(650)
      await expect(counter).toHaveText(frozen!)
      await page.screenshot({
        animations: 'disabled',
        path: `/home/cristhian/excalibur-sim/scratch/run-state/${viewport.width}-paused-closed.png`,
      })
      await page.locator('.lab-chat-toggle').click()
      if (viewport.width === 390) {
        const chatRun = page.locator('.lab-chat .lab-run-controls')
        await expect(chatRun.locator('.lab-run-status')).toBeVisible()
        await expect(chatRun.locator('.lab-run-status')).toHaveText(/Paused|En pausa/)
        await chatRun.getByRole('button', { name: /Resume simulation|Reanudar simulación/ }).click()
        await expect(chatRun.locator('.lab-run-status')).toHaveText(/Running|En ejecución/)
        await chatRun.getByRole('button', { name: /Pause simulation|Pausar simulación/ }).click()
        await expect(chatRun.locator('.lab-run-status')).toHaveText(/Paused|En pausa/)
        frozen = await counter.textContent()
      }
      await page.screenshot({
        animations: 'disabled',
        path: `/home/cristhian/excalibur-sim/scratch/run-state/${viewport.width}-paused-open.png`,
      })
      if (viewport.width === 390)
        await page.getByRole('button', { name: /Back to Robot Lab|Volver al laboratorio/ }).click()
      const hud = page.locator('.lab-live-hud')
      await expect(hud.locator('.lab-run-status')).toHaveText(/Paused|En pausa/)
      await hud.getByRole('button', { name: /Resume simulation|Reanudar simulación/ }).click()
      await expect(hud.locator('.lab-run-status')).toHaveText(/Running|En ejecución/)
      await expect
        .poll(async () =>
          Number(await page.locator('.lab-live-hud > span strong').first().textContent()),
        )
        .toBeGreaterThan(Number(frozen))
      await page.emulateMedia({ reducedMotion: 'reduce' })
      await expect(hud.locator('.lab-run-status > svg')).toHaveCSS('animation-name', 'none')
      await page.screenshot({
        animations: 'disabled',
        path: `/home/cristhian/excalibur-sim/scratch/run-state/${viewport.width}-running.png`,
      })
      await page.evaluate(() => document.documentElement.classList.add('dark'))
      await page.screenshot({
        animations: 'disabled',
        path: `/home/cristhian/excalibur-sim/scratch/run-state/${viewport.width}-dark.png`,
      })
    } finally {
      clearInterval(timer)
    }
  })
}
