import '@/lib/i18n'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import type { Scene } from './contracts'
const mocks = vi.hoisted(() => ({ call: vi.fn() }))
vi.mock('./api', () => ({ robotlabCall: mocks.call, robotlabEvents: () => () => {} }))
vi.mock('./MappingEditor', () => ({ MappingEditor: () => null }))
vi.mock('./overlay', () => ({ drawOverlay: vi.fn() }))
import { TeleopPanel } from './TeleopPanel'
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  vi.resetAllMocks()
})
it('announces camera off, starting, on and stopping with a single toggle', async () => {
  const stop = vi.fn()
  let grant: (stream: MediaStream) => void = () => {}
  Object.defineProperty(navigator, 'mediaDevices', {
    configurable: true,
    value: {
      getUserMedia: () =>
        new Promise<MediaStream>((done) => {
          grant = done
        }),
      enumerateDevices: async () => [],
    },
  })
  vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue()
  const track = { stop, addEventListener: vi.fn() }
  const stream = {
    getTracks: () => [track],
    getVideoTracks: () => [track],
  } as unknown as MediaStream
  let idle: () => void = () => {}
  mocks.call.mockImplementation(
    () =>
      new Promise<void>((done) => {
        idle = done
      }),
  )
  render(
    <TeleopPanel
      session="s"
      scene={{ scenario: 'arm' } as Scene}
      sendPose={vi.fn()}
      viewer={<div />}
      trackerFactory={async () => ({ detect: vi.fn(), close: vi.fn() })}
    />,
  )
  expect(screen.getByText(/Camera off|Cámara apagada/)).toBeVisible()
  fireEvent.click(screen.getByRole('button', { name: /Enable camera|Activar cámara/ }))
  expect(screen.getByRole('button', { name: /Starting|Iniciando/ })).toBeDisabled()
  await act(async () => grant(stream))
  const toggle = await screen.findByRole('button', { name: /Turn camera off|Apagar cámara/ })
  expect(toggle).toHaveAttribute('aria-pressed', 'true')
  expect(screen.getByText(/Camera on|Cámara activa/)).toBeVisible()
  fireEvent.click(toggle)
  expect(stop).toHaveBeenCalledOnce()
  expect(screen.getByRole('button', { name: /Stopping|Deteniendo/ })).toBeDisabled()
  await act(async () => idle())
  await waitFor(() =>
    expect(screen.getByRole('button', { name: /Enable camera|Activar cámara/ })).toBeEnabled(),
  )
})
