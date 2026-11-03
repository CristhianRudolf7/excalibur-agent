import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import i18n from '@/lib/i18n'
import { rpc } from '@/lib/rpc/client'
import type { RobotCalibrationStatus, RobotImageSize } from '@/lib/rpc/contract'
import { CameraCard } from './CameraCard'

const bus = vi.hoisted(() => ({ event: (_event: unknown) => {} }))
vi.mock('@/lib/rpc/events', () => ({
  subscribeEvents: (options: { event: (event: unknown) => void }) => {
    bus.event = options.event
    return () => {}
  },
}))
vi.mock('./camera', () => ({
  cameraErrorKey: () => 'robotCameraFailure',
  RobotCamera: class {
    events: { sent: (at: number, image: RobotImageSize) => void }
    constructor(events: { sent: (at: number, image: RobotImageSize) => void }) {
      this.events = events
    }
    async start() {
      this.events.sent(Date.now(), { width: 1280, height: 720 })
      return true
    }
    stop() {}
    setInterval() {}
  },
}))

let status: RobotCalibrationStatus
let failPoint = false
let client: QueryClient
beforeEach(async () => {
  await i18n.changeLanguage('en')
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  status = {
    calibrated: false,
    points: [],
    table_z: 0,
    image: { width: 0, height: 0 },
    reprojection_error_px: 0,
  }
  failPoint = false
  Object.defineProperty(HTMLElement.prototype, 'scrollIntoView', {
    configurable: true,
    value: vi.fn(),
  })
  vi.spyOn(HTMLVideoElement.prototype, 'getBoundingClientRect').mockReturnValue({
    left: 100,
    top: 100,
    width: 640,
    height: 480,
  } as DOMRect)
  vi.spyOn(rpc, 'call').mockImplementation((method, params) => {
    if (method === 'robot.calibration.add_point') {
      if (failPoint) return Promise.reject(new Error('TCP unavailable; reconnect the arm'))
      const { pixel } = params as { pixel: [number, number] }
      status = {
        ...status,
        image: { width: 1280, height: 720 },
        points: [...status.points, { pixel, base: [0.3214, -0.1896] }],
      }
    }
    if (method === 'robot.calibration.set_table_z') status = { ...status, table_z: 0.1234 }
    if (method === 'robot.calibration.clear')
      status = { ...status, calibrated: false, points: [], table_z: 0 }
    return Promise.resolve(structuredClone(status)) as never
  })
})
afterEach(() => {
  cleanup()
  client.clear()
  vi.restoreAllMocks()
  Reflect.deleteProperty(HTMLElement.prototype, 'scrollIntoView')
})

function view(connected = true) {
  return (
    <QueryClientProvider client={client}>
      <CameraCard connected={connected} />
    </QueryClientProvider>
  )
}
async function startMarking() {
  fireEvent.click(screen.getByRole('button', { name: 'Start camera' }))
  const mark = screen.getByRole('button', { name: 'Mark point' })
  await waitFor(() => expect(mark).toBeEnabled())
  fireEvent.click(mark)
  return screen.getByRole('button', { name: 'Add camera point (keyboard: center)' })
}

it('toggles mark mode by button and Escape, and exits on camera stop or disconnect', async () => {
  const { rerender } = render(view())
  expect(screen.getByRole('button', { name: 'Mark point' })).toBeDisabled()
  await startMarking()
  const card = screen.getByRole('region', { name: 'Camera calibration' })
  expect(within(card).getByRole('button', { name: 'Exit mark mode' })).toHaveAttribute(
    'aria-pressed',
    'true',
  )
  fireEvent.keyDown(window, { key: 'Escape' })
  expect(screen.queryByRole('button', { name: /Add camera point/ })).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: 'Mark point' }))
  fireEvent.click(within(card).getByRole('button', { name: 'Exit mark mode' }))
  expect(screen.getByRole('button', { name: 'Mark point' })).toHaveAttribute(
    'aria-pressed',
    'false',
  )
  fireEvent.click(screen.getByRole('button', { name: 'Mark point' }))
  fireEvent.click(screen.getByRole('button', { name: 'Stop camera' }))
  expect(screen.queryByRole('button', { name: /Add camera point/ })).toBeNull()
  await startMarking()
  rerender(view(false))
  expect(screen.queryByRole('button', { name: /Add camera point/ })).toBeNull()
  expect(screen.getByRole('button', { name: 'Use current height as table' })).toBeDisabled()
  rerender(view())
  expect(screen.getByRole('button', { name: 'Mark point' })).toHaveAttribute(
    'aria-pressed',
    'false',
  )
})

it('submits scaled frame pixels, ignores letterboxes, draws markers and sets current height', async () => {
  const { container } = render(view())
  const preview = await startMarking()
  fireEvent.click(preview, { detail: 1, clientX: 420, clientY: 120 })
  expect(rpc.call).not.toHaveBeenCalledWith('robot.calibration.add_point', expect.anything())
  fireEvent.click(preview, { detail: 1, clientX: 420, clientY: 340 })
  await waitFor(() =>
    expect(rpc.call).toHaveBeenCalledWith('robot.calibration.add_point', { pixel: [640, 360] }),
  )
  await waitFor(() =>
    expect(container.querySelector('.robot-calibration-markers text')).toHaveTextContent('1'),
  )
  expect(screen.getByText('(0.321, -0.190) m')).toBeVisible()
  fireEvent.click(screen.getByRole('button', { name: 'Use current height as table' }))
  await waitFor(() =>
    expect(rpc.call).toHaveBeenCalledWith('robot.calibration.set_table_z', { use_current: true }),
  )
  await waitFor(() => expect(screen.getByText('0.123 m')).toBeVisible())
})

it('requires clear confirmation and keeps a rejected point recoverable', async () => {
  render(view())
  const preview = await startMarking()
  failPoint = true
  fireEvent.click(preview)
  await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('TCP unavailable'))
  failPoint = false
  fireEvent.click(screen.getByRole('button', { name: 'Refresh status' }))
  await waitFor(() => expect(screen.queryByRole('alert')).toBeNull())
  fireEvent.click(preview)
  await waitFor(() => expect(screen.getByText('1 · minimum 4')).toBeVisible())
  fireEvent.click(screen.getByRole('button', { name: 'Clear calibration' }))
  expect(rpc.call).not.toHaveBeenCalledWith('robot.calibration.clear')
  fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
  expect(screen.queryByRole('button', { name: 'Confirm clear' })).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: 'Clear calibration' }))
  fireEvent.click(screen.getByRole('button', { name: 'Confirm clear' }))
  await waitFor(() => expect(screen.getByText('0 · minimum 4')).toBeVisible())
  expect(screen.getByRole('button', { name: 'Mark point' })).toHaveAttribute(
    'aria-pressed',
    'false',
  )
})

it('updates calibration from daemon events and translates the guided steps into Spanish', async () => {
  await i18n.changeLanguage('es')
  render(view(false))
  await screen.findByText('Sin calibrar')
  await act(async () =>
    bus.event({
      type: 'robot.state',
      payload: {
        calibration: { ...status, calibrated: true, table_z: 0.2, reprojection_error_px: 0.125 },
      },
    }),
  )
  expect(await screen.findByText('Calibrada')).toBeVisible()
  expect(screen.getByText('0.13 px')).toBeVisible()
  expect(screen.getByText('0.200 m')).toBeVisible()
  expect(screen.getByText('Coloca una pequeña marca en la mesa.')).toBeVisible()
  expect(screen.getByRole('button', { name: 'Marcar punto' })).toBeDisabled()
  expect(screen.getByRole('button', { name: 'Borrar calibración' })).toBeDisabled()
})
