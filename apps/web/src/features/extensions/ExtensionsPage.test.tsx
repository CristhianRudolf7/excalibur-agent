import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import i18n from '@/lib/i18n'
import { ExtensionsPage } from './ExtensionsPage'
import { extensionCatalog } from './catalog'
import { installedExtensionsKey, useExtensions } from './store'

const navigate = vi.hoisted(() => vi.fn())
vi.mock('@tanstack/react-router', () => ({ useNavigate: () => navigate }))
beforeEach(async () => {
  await i18n.changeLanguage('es')
  useExtensions.setState({ installed: extensionCatalog.map(({ id }) => id) })
  navigate.mockClear()
})
afterEach(async () => {
  cleanup()
  localStorage.removeItem(installedExtensionsKey)
  await i18n.changeLanguage('es')
})

it('shows only the real built-ins and provides the extension kit empty state', () => {
  render(<ExtensionsPage />)
  expect(screen.getAllByRole('article')).toHaveLength(3)
  fireEvent.click(screen.getByRole('button', { name: 'Disponibles 0' }))
  expect(screen.queryByRole('article')).toBeNull()
  expect(screen.getByText(/excalibur-extension.yaml/)).toHaveTextContent(
    'docs/development/extending.md',
  )
})

it('combines accent-insensitive search and categories and clears empty filters', () => {
  render(<ExtensionsPage />)
  const search = screen.getByRole('searchbox', { name: 'Buscar extensiones' })
  fireEvent.change(search, { target: { value: 'ROBOTICO' } })
  expect(screen.getAllByRole('article')).toHaveLength(2)
  fireEvent.click(screen.getByRole('button', { name: 'Educación' }))
  expect(screen.queryByRole('article')).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: 'Limpiar filtros' }))
  expect(search).toHaveValue('')
  expect(screen.getAllByRole('article')).toHaveLength(3)
})

it('uninstalls and reinstalls a built-in through the available catalog', () => {
  render(<ExtensionsPage />)
  fireEvent.click(
    within(screen.getByRole('article', { name: 'Brazo robótico' })).getByRole('button', {
      name: 'Desinstalar',
    }),
  )
  expect(screen.queryByRole('article', { name: 'Brazo robótico' })).toBeNull()
  expect(screen.getByRole('status')).toHaveTextContent('Se desinstaló Brazo robótico.')
  expect(useExtensions.getState().installed).not.toContain('robot')
  fireEvent.click(screen.getByRole('button', { name: 'Disponibles 1' }))
  fireEvent.click(screen.getByRole('button', { name: 'Instalar' }))
  expect(useExtensions.getState().installed).toContain('robot')
  expect(screen.getByRole('status')).toHaveTextContent('Se instaló Brazo robótico.')
})

it.each(extensionCatalog)('opens $id at its unchanged route', (extension) => {
  render(<ExtensionsPage />)
  fireEvent.click(
    within(screen.getByRole('article', { name: i18n.t(extension.nameKey) })).getByRole('button', {
      name: 'Abrir',
    }),
  )
  expect(navigate).toHaveBeenCalledWith({ to: extension.route })
})

it('translates the page, cards, categories and controls to English', async () => {
  await i18n.changeLanguage('en')
  render(<ExtensionsPage />)
  expect(screen.getByRole('heading', { level: 1, name: 'Extensions' })).toBeInTheDocument()
  expect(screen.getByRole('searchbox', { name: 'Search extensions' })).toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Education' })).toBeInTheDocument()
  const card = screen.getByRole('article', { name: 'Robot arm' })
  expect(within(card).getByRole('button', { name: 'Uninstall' })).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'Available 0' }))
  expect(screen.getByText(/New extensions are built with the extension kit/)).toBeInTheDocument()
})
