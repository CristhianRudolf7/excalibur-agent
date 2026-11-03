import { beforeEach, afterEach, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import '@/lib/i18n'
import { SidebarRail } from './SidebarRail'
import { defaultAgent } from '@/features/agents/store'
import { useExtensions } from '@/features/extensions/store'
import { extensionCatalog } from '@/features/extensions/catalog'

vi.mock('@/features/profile/api', () => ({
  useProfile: () => ({ data: { id: 'local', name: 'Ana Prueba' }, isPending: false }),
}))
beforeEach(() => useExtensions.setState({ installed: extensionCatalog.map(({ id }) => id) }))
afterEach(cleanup)
function props() {
  return {
    agents: [defaultAgent],
    selectedAgentId: 'excalibur',
    activeItem: 'world',
    onNavigate: vi.fn(),
    onAgent: vi.fn(),
    onCreateAgent: vi.fn(),
    onProfile: vi.fn(),
  }
}
it('renders the single agent, real profile initials, badges and active items', () => {
  const { container } = render(<SidebarRail {...props()} />)
  expect(screen.getByRole('complementary', { name: 'Navegación compacta' })).toBeInTheDocument()
  const agents = screen.getByRole('navigation', { name: 'Agentes' })
  expect(agents.querySelectorAll('.pixel-avatar')).toHaveLength(1)
  expect(screen.getByRole('button', { name: 'Excalibur' })).toHaveAttribute('aria-pressed', 'true')
  expect(screen.queryByRole('button', { name: 'Crear agente' })).toBeNull()
  expect(screen.getByRole('button', { name: 'Mundo 2D' })).toHaveAttribute('aria-pressed', 'true')
  expect(container.querySelectorAll('.sidebar-rail-working')).toHaveLength(0)
  expect(container.querySelectorAll('.sidebar-rail-unread')).toHaveLength(0)
  expect(screen.getByRole('button', { name: 'Perfil' })).toHaveTextContent('AP')
  expect(screen.queryByText('Ana Prueba')).toBeNull()
  expect(screen.queryByRole('button', { name: 'Bandeja de entrada' })).toBeNull()
  expect(screen.queryByRole('button', { name: 'Organigrama' })).toBeNull()
})
it('routes every navigation and action through its supplied handler', () => {
  const handlers = props()
  render(<SidebarRail {...handlers} />)
  for (const [label, callback] of [['Perfil', handlers.onProfile]] as const) {
    fireEvent.click(screen.getByRole('button', { name: label }))
    expect(callback).toHaveBeenCalledOnce()
  }
  for (const [label, key] of [
    ['Mis tareas', 'myTasks'],
    ['Recordatorios', 'reminders'],
    ['Proyectos', 'projects'],
    ['Mundo 2D', 'world'],
    ['Aula', 'classroom'],
    ['Brazo robótico', 'robot'],
    ['Extensiones', 'extensions'],
    ['Laboratorio robótico', 'lab_title'],
    ['Automatizaciones', 'automations'],
    ['Configuración', 'settings'],
  ]) {
    fireEvent.click(screen.getByRole('button', { name: label }))
    expect(handlers.onNavigate).toHaveBeenLastCalledWith(key)
  }
  expect(screen.queryByRole('button', { name: 'Métricas' })).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: 'Excalibur' }))
  expect(handlers.onAgent).toHaveBeenCalledWith('excalibur')
})
it('clears agent selection outside conversations and highlights the current page', () => {
  render(<SidebarRail {...props()} selectedAgentId={undefined} activeItem="projects" />)
  expect(screen.getByRole('button', { name: 'Proyectos' })).toHaveAttribute('aria-pressed', 'true')
  expect(screen.getByRole('button', { name: 'Excalibur' })).toHaveAttribute('aria-pressed', 'false')
})

it('highlights the classroom entry independently of the agent', () => {
  render(<SidebarRail {...props()} selectedAgentId={undefined} activeItem="classroom" />)
  expect(screen.getByRole('button', { name: 'Aula' })).toHaveAttribute('aria-pressed', 'true')
})
it('highlights Robot without selecting the agent', () => {
  render(<SidebarRail {...props()} selectedAgentId={undefined} activeItem="robot" />)
  expect(screen.getByRole('button', { name: 'Brazo robótico' })).toHaveAttribute(
    'aria-pressed',
    'true',
  )
  expect(screen.getByRole('button', { name: 'Excalibur' })).toHaveAttribute('aria-pressed', 'false')
})
it('routes the selected Robot Lab rail item with current-page semantics', () => {
  const options = { ...props(), activeItem: 'lab_title' }
  render(<SidebarRail {...options} />)
  const lab = screen.getByRole('button', { name: 'Laboratorio robótico' })
  expect(lab).toHaveAttribute('aria-current', 'page')
  expect(
    within(screen.getByRole('group', { name: 'Instaladas' })).getByRole('button', {
      name: 'Laboratorio robótico',
    }),
  ).toBe(lab)
  fireEvent.click(lab)
  expect(options.onNavigate).toHaveBeenCalledWith('lab_title')
})

it('keeps agents, work, extensions and bottom settings in the specified order', () => {
  render(<SidebarRail {...props()} />)
  expect(screen.getAllByRole('button').map((button) => button.getAttribute('aria-label'))).toEqual([
    'Excalibur',
    'Mis tareas',
    'Recordatorios',
    'Proyectos',
    'Mundo 2D',
    'Automatizaciones',
    'Extensiones',
    'Aula',
    'Laboratorio robótico',
    'Brazo robótico',
    'Configuración',
    'Perfil',
  ])
  expect(screen.getAllByRole('separator')).toHaveLength(3)
  expect(screen.queryByRole('button', { name: 'Herramientas' })).toBeNull()
  expect(screen.queryByRole('button', { name: 'Tiendas' })).toBeNull()
})

it('only lists installed extensions in the extensions group', () => {
  useExtensions.setState({ installed: ['classroom'] })
  render(<SidebarRail {...props()} />)
  expect(screen.getByRole('button', { name: 'Aula' })).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: 'Laboratorio robótico' })).toBeNull()
  expect(screen.queryByRole('button', { name: 'Brazo robótico' })).toBeNull()
  expect(screen.getByRole('button', { name: 'Extensiones' })).toBeInTheDocument()
})
