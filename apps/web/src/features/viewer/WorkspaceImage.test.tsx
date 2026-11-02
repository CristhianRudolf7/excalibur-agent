import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import i18n from '@/lib/i18n'
import { WorkspaceImage } from './WorkspaceImage'
afterEach(cleanup)
it('zooms images, fits their width and supplies a keyboard-accessible pan viewport', async () => {
  await i18n.changeLanguage('en')
  render(<WorkspaceImage url="blob:image" path="Architecture" interactive />)
  const image = screen.getByRole('img')
  expect(image).toHaveStyle({ width: '100%' })
  fireEvent.click(screen.getByRole('button', { name: 'Zoom in' }))
  expect(image).toHaveStyle({ width: '125%' })
  expect(screen.getByRole('region')).toHaveAttribute('tabindex', '0')
  fireEvent.click(screen.getByRole('button', { name: 'Fit width' }))
  expect(image).toHaveStyle({ width: '100%' })
  fireEvent.click(screen.getByRole('button', { name: 'Zoom out' }))
  expect(image).toHaveStyle({ width: '75%' })
  fireEvent.error(image)
  expect(screen.getByRole('alert')).toHaveTextContent('Could not open this file')
})
