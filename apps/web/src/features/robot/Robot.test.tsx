import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import i18n from '@/lib/i18n'
import { rpc } from '@/lib/rpc/client'
import { useShell } from '@/features/shell/store'
import { Robot } from './Robot'

vi.mock('./api', () => ({
  useRobot: () => ({ query: { data: { state: { connected: false } } }, connection: {} }),
}))
vi.mock('./CameraCard', () => ({ CameraCard: () => <div>Camera preview</div> }))
vi.mock('@/features/agents/AgentSwitcher', () => ({
  AgentSwitcher: () => null,
}))
vi.mock('@/features/chat/Chat', () => ({
  Chat: ({ agentId }: { agentId: string }) => <textarea aria-label={`Chat ${agentId}`} />,
}))

let width = 1200
let resize: () => void
beforeEach(async () => {
  await i18n.changeLanguage('en')
  width = 1200
  useShell.setState({ ratio: 0.5 })
  vi.stubGlobal('matchMedia', () => ({
    matches: width < 1024,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  }))
  vi.spyOn(rpc, 'call').mockResolvedValue({ ok: true } as never)
  vi.stubGlobal(
    'ResizeObserver',
    class {
      constructor(callback: ResizeObserverCallback) {
        resize = () =>
          callback(
            [{ contentRect: { width } } as ResizeObserverEntry],
            this as unknown as ResizeObserver,
          )
      }
      observe() {
        resize()
      }
      disconnect() {}
    },
  )
})
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

it('toggles the Excalibur chat on the left and restores the full robot workspace', () => {
  const { container } = render(<Robot />)
  const toggle = screen.getByRole('button', { name: 'Open chat' })
  expect(toggle).toHaveAttribute('aria-expanded', 'false')
  expect(toggle).toHaveAttribute('aria-pressed', 'false')
  expect(screen.queryByRole('textbox', { name: 'Chat excalibur' })).toBeNull()
  const host = screen.getByRole('textbox', { name: 'Arm IP address' })
  fireEvent.change(host, { target: { value: '192.168.1.20' } })
  fireEvent.click(toggle)
  expect(toggle).toHaveAccessibleName('Close chat')
  expect(toggle).toHaveAttribute('aria-expanded', 'true')
  expect(toggle).toHaveAttribute('aria-pressed', 'true')
  expect(screen.getByRole('textbox', { name: 'Chat excalibur' })).toBeVisible()
  expect(screen.getByRole('region', { name: 'Arm connection' })).toBeVisible()
  expect(container.querySelector('.robot-layout')?.firstElementChild).toHaveAttribute(
    'id',
    'robot-conversation',
  )
  expect(container.querySelector('.robot-workspace')).not.toHaveAttribute('inert')
  expect(screen.getByRole('separator', { name: 'Resize panels' })).toBeVisible()
  expect(screen.getByRole('button', { name: 'STOP' })).toBeEnabled()
  fireEvent.click(toggle)
  expect(toggle).toHaveAccessibleName('Open chat')
  expect(screen.queryByRole('separator')).toBeNull()
  expect(screen.queryByRole('region', { name: 'Excalibur chat' })).toBeNull()
  expect(host).toHaveValue('192.168.1.20')
  expect(screen.getByText('Camera preview')).toBeVisible()
})

it('resizes through the shared shell ratio and resets to equal halves', () => {
  render(<Robot />)
  fireEvent.click(screen.getByRole('button', { name: 'Open chat' }))
  const separator = screen.getByRole('separator')
  fireEvent.keyDown(separator, { key: 'ArrowLeft' })
  expect(useShell.getState().ratio).toBeLessThan(0.5)
  fireEvent.doubleClick(separator)
  expect(useShell.getState().ratio).toBe(0.5)
})

it('uses a closable narrow overlay while leaving STOP operable and restoring focus', () => {
  width = 390
  const { container } = render(<Robot />)
  const toggle = screen.getByRole('button', { name: 'Open chat' })
  toggle.focus()
  fireEvent.click(toggle)
  const chat = screen.getByRole('region', { name: 'Excalibur chat' })
  expect(chat).toHaveClass('robot-chat-overlay')
  expect(container.querySelector('.robot-workspace')).toHaveAttribute('inert')
  expect(screen.queryByRole('separator')).toBeNull()
  const stop = screen.getByRole('button', { name: 'STOP' })
  expect(stop.closest('[inert]')).toBeNull()
  fireEvent.click(stop)
  expect(rpc.call).toHaveBeenCalledWith('robot.stop', {})
  fireEvent.click(chat.querySelector('button')!)
  expect(container.querySelector('.robot-workspace')).not.toHaveAttribute('inert')
  expect(toggle).toHaveFocus()
})

it('responds to container resizing and keeps chat keystrokes separate from robot shortcuts', () => {
  const { container } = render(<Robot />)
  fireEvent.click(screen.getByRole('button', { name: 'Open chat' }))
  const input = screen.getByRole('textbox', { name: 'Chat excalibur' })
  input.focus()
  fireEvent.keyDown(input, { key: ' ', code: 'Space' })
  fireEvent.keyDown(input, { key: 'Escape' })
  expect(rpc.call).not.toHaveBeenCalled()
  width = 600
  act(() => resize())
  expect(container.querySelector('.robot-chat')).toHaveClass('robot-chat-overlay')
  width = 1200
  act(() => resize())
  expect(container.querySelector('.robot-chat')).not.toHaveClass('robot-chat-overlay')
  expect(container.querySelector('.robot-workspace')).not.toHaveAttribute('inert')
  container.querySelector<HTMLElement>('.robot-page')!.focus()
  fireEvent.keyDown(window, { key: 'Escape' })
  expect(rpc.call).toHaveBeenCalledWith('robot.stop', {})
})

it('translates the toggle labels into Spanish', async () => {
  await i18n.changeLanguage('es')
  render(<Robot />)
  fireEvent.click(screen.getByRole('button', { name: 'Abrir chat' }))
  expect(screen.getByRole('button', { name: 'Cerrar chat' })).toHaveAttribute(
    'aria-expanded',
    'true',
  )
})
