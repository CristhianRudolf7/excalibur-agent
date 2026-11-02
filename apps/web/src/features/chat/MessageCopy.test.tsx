import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import '@/lib/i18n'
import { MessageCopy } from './MessageCopy'

const clipboard = Object.getOwnPropertyDescriptor(navigator, 'clipboard')

afterEach(() => {
  if (clipboard) Object.defineProperty(navigator, 'clipboard', clipboard)
  else Reflect.deleteProperty(navigator, 'clipboard')
  cleanup()
  vi.restoreAllMocks()
})

it('copies the complete Markdown source and announces success', async () => {
  const writeText = vi.fn().mockResolvedValue(undefined)
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
  render(<MessageCopy text={'## Plan\n\n- Review'} />)
  fireEvent.click(screen.getByRole('button', { name: 'Copiar mensaje' }))
  await waitFor(() => expect(screen.getByRole('button')).toHaveTextContent('Copiado'))
  expect(writeText).toHaveBeenCalledWith('## Plan\n\n- Review')
})

it('announces clipboard failure and remains available to retry', async () => {
  const writeText = vi.fn().mockRejectedValueOnce(new Error('Denied')).mockResolvedValue(undefined)
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
  render(<MessageCopy text="Answer" />)
  fireEvent.click(screen.getByRole('button'))
  await waitFor(() => expect(screen.getByRole('button')).toHaveTextContent('No se pudo copiar'))
  fireEvent.click(screen.getByRole('button'))
  await waitFor(() => expect(screen.getByRole('button')).toHaveTextContent('Copiado'))
})
