import { afterEach, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import '@/lib/i18n'
import { SidebarRail } from './SidebarRail'
import { getAgents } from './mock-data'

vi.mock('@/features/profile/api', () => ({
  useProfile: () => ({ data: { id: 'local', name: 'Ana Prueba' }, isPending: false }),
}))
afterEach(cleanup)
function props() {
  return {
    agents: getAgents(),
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
    ['Robot', 'robot'],
    ['Tiendas', 'stores'],
    ['Herramientas', 'tools'],
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
  expect(screen.getByRole('button', { name: 'Robot' })).toHaveAttribute('aria-pressed', 'true')
  expect(screen.getByRole('button', { name: 'Excalibur' })).toHaveAttribute('aria-pressed', 'false')
})
