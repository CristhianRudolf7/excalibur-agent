import '@/lib/i18n'
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
const mocks = vi.hoisted(() => ({ call: vi.fn() }))
vi.mock('../teleop/api', () => ({ robotlabCall: mocks.call, robotlabEvents: () => () => {} }))
import { VisionPanel } from './VisionPanel'
afterEach(() => {
  cleanup()
  vi.resetAllMocks()
})
it('announces a busy step and exposes errors after the call finishes', async () => {
  let reject: (error: Error) => void = () => {}
  mocks.call.mockImplementation(
    () =>
      new Promise((_done, fail) => {
        reject = fail
      }),
  )
  render(<VisionPanel session="s" />)
  fireEvent.change(screen.getByRole('textbox'), { target: { value: 'Pick up the cube' } })
  fireEvent.click(screen.getByRole('button', { name: /Run step|Ejecutar paso/ }))
  expect(screen.getByRole('button', { name: /Starting|Iniciando/ })).toBeDisabled()
  expect(
    screen.getAllByRole('status').find((node) => node.hasAttribute('data-state')),
  ).toHaveAttribute('data-state', 'starting')
  await act(async () => reject(new Error('offline')))
  expect(
    screen.getAllByRole('status').find((node) => node.hasAttribute('data-state')),
  ).toHaveAttribute('data-state', 'error')
  expect(screen.getByRole('button', { name: /Run step|Ejecutar paso/ })).toBeEnabled()
  expect(screen.getByRole('button', { name: /Run step|Ejecutar paso/ })).not.toHaveAttribute(
    'aria-pressed',
  )
})
