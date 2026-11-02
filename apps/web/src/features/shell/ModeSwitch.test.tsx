import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import '@/lib/i18n'
import { ModeSwitch } from './ModeSwitch'

afterEach(cleanup)
it('compact mode exposes translated icon labels, shortcut hints and selection', () => {
  const change = vi.fn()
  render(<ModeSwitch variant="compact" mode="chat" onChange={change} />)
  const chat = screen.getByRole('button', { name: 'Chat' })
  const world = screen.getByRole('button', { name: 'Mundo 2D' })
  expect(chat).toHaveAttribute('aria-pressed', 'true')
  expect(world).toHaveAttribute('aria-pressed', 'false')
  expect(world).toHaveAttribute('title', 'Mundo 2D (Ctrl+J)')
  expect(world.querySelector('svg')).not.toBeNull()
  fireEvent.click(world)
  expect(change).toHaveBeenCalledWith('world')
})
