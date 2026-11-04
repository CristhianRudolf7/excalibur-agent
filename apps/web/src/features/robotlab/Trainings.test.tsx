import { beforeEach, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import i18n from '@/lib/i18n'
import { NewSessionDialog, Trainings } from './Trainings'
const mocks = vi.hoisted(() => ({ call: vi.fn(), send: vi.fn() }))
vi.mock('./api', () => ({ labCall: mocks.call }))
vi.mock('@/features/chat/live', () => ({ sendMessage: mocks.send }))
function mount(node: React.ReactNode) {
  return render(
    <QueryClientProvider
      client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
    >
      {node}
    </QueryClientProvider>,
  )
}
beforeEach(async () => {
  cleanup()
  vi.clearAllMocks()
  await i18n.changeLanguage('en')
  HTMLDialogElement.prototype.showModal = function () {
    this.setAttribute('open', '')
  }
  HTMLDialogElement.prototype.close = function () {
    this.removeAttribute('open')
  }
  mocks.send.mockResolvedValue(true)
  mocks.call.mockImplementation(async (method: string) => {
    if (method === 'robotlab.robots.list') return { robots: ['arm', 'dog'].map((id) => ({ id })) }
    if (method === 'robotlab.env.list')
      return {
        environments: [
          { id: 'room', name: 'Room', kind: 'room' },
          { id: 'table', name: 'Table', kind: 'table' },
        ],
      }
    if (method === 'robotlab.session.list')
      return {
        sessions: [
          {
            id: 'old',
            name: 'Old session',
            robot: 'arm',
            environment: 'table',
            task: 'Move cube',
            status: 'stopped',
            attempts: 2,
            created_at: '2026-01-01',
          },
          {
            id: 'live',
            name: 'Live session',
            robot: 'dog',
            environment: 'room',
            task: 'Fetch ball',
            status: 'live',
            attempts: 1,
            created_at: '2025-01-01',
          },
        ],
      }
    return {}
  })
})
it.each(['en', 'es'])(
  'sends a localized session instruction with display names (%s)',
  async (language) => {
    await i18n.changeLanguage(language)
    const sent = vi.fn()
    mount(<NewSessionDialog onClose={vi.fn()} onCreateEnvironment={vi.fn()} onSent={sent} />)
    await screen.findByRole('option', {
      name: language === 'es' ? 'Perro' : 'Dog',
    })
    fireEvent.change(screen.getByLabelText('Robot'), { target: { value: 'dog' } })
    expect(screen.queryByRole('option', { name: 'Table' })).not.toBeInTheDocument()
    fireEvent.change(screen.getByLabelText(language === 'es' ? 'Ambiente' : 'Environment'), {
      target: { value: 'room' },
    })
    fireEvent.change(screen.getByLabelText(language === 'es' ? 'Tarea' : 'Task'), {
      target: { value: 'Fetch the ball' },
    })
    fireEvent.click(
      screen.getByRole('button', {
        name: language === 'es' ? 'Enviar al agente' : 'Send to agent',
      }),
    )
    await waitFor(() =>
      expect(mocks.send).toHaveBeenCalledWith(
        'excalibur',
        language === 'es'
          ? 'Realiza una simulación con el Perro en el escenario Room: Fetch the ball'
          : 'Run a simulation with the Dog in the environment Room: Fetch the ball',
      ),
    )
    expect(sent).toHaveBeenCalled()
    expect(mocks.call.mock.calls.some(([method]) => method === 'robotlab.session.start')).toBe(
      false,
    )
  },
)
it('switches to environment creation without starting a session', async () => {
  const create = vi.fn()
  mount(<NewSessionDialog onClose={vi.fn()} onCreateEnvironment={create} onSent={vi.fn()} />)
  fireEvent.change(screen.getByLabelText('Environment'), { target: { value: '__new' } })
  expect(create).toHaveBeenCalled()
  expect(mocks.send).not.toHaveBeenCalled()
})
it('shows the live session first and opens old sessions by id', async () => {
  const select = vi.fn()
  mount(<Trainings onSelect={select} onCreateEnvironment={vi.fn()} onAgent={vi.fn()} />)
  await screen.findByRole('button', { name: 'Live session' })
  const rows = screen.getAllByRole('row')
  expect(rows[1]).toHaveTextContent('Fetch ball')
  expect(rows[1]).toHaveTextContent('Room')
  expect(rows[2]).toHaveTextContent('Move cube')
  expect(rows[2]).toHaveTextContent('Table')
  fireEvent.click(screen.getByRole('button', { name: 'Old session' }))
  expect(select).toHaveBeenCalledWith('old')
})
it('falls back to environment ids when the environment list is unavailable', async () => {
  const original = mocks.call.getMockImplementation()!
  mocks.call.mockImplementation((method: string, ...args: unknown[]) => {
    if (method === 'robotlab.env.list') return Promise.reject(new Error('offline'))
    return original(method, ...args)
  })
  mount(<Trainings onSelect={vi.fn()} onCreateEnvironment={vi.fn()} onAgent={vi.fn()} />)
  await screen.findByRole('button', { name: 'Live session' })
  expect(screen.getAllByRole('row')[1]).toHaveTextContent('room')
  expect(screen.getAllByRole('row')[2]).toHaveTextContent('table')
})
it('keeps a failed chat send visible for retry', async () => {
  mocks.send.mockResolvedValue(false)
  mount(<NewSessionDialog onClose={vi.fn()} onCreateEnvironment={vi.fn()} onSent={vi.fn()} />)
  await screen.findByRole('option', { name: 'Arm' })
  fireEvent.change(screen.getByLabelText('Robot'), { target: { value: 'arm' } })
  fireEvent.change(screen.getByLabelText('Environment'), { target: { value: 'table' } })
  fireEvent.change(screen.getByLabelText('Task'), { target: { value: 'Move cube' } })
  fireEvent.click(screen.getByRole('button', { name: 'Send to agent' }))
  expect(await screen.findByRole('alert')).toHaveTextContent('Message could not be sent')
})

it.each(['en', 'es'])('offers exactly the two supported robots (%s)', async (language) => {
  await i18n.changeLanguage(language)
  mount(<NewSessionDialog onClose={vi.fn()} onCreateEnvironment={vi.fn()} onSent={vi.fn()} />)
  await screen.findByRole('option', { name: language === 'es' ? 'Perro' : 'Dog' })
  const select = screen.getByLabelText('Robot') as HTMLSelectElement
  expect(Array.from(select.options).map((option) => option.value)).toEqual(['', 'arm', 'dog'])
})
