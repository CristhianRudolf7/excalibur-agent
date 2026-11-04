import { beforeEach, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import '@/lib/i18n'
import i18n from '@/lib/i18n'
import { Environments, EnvironmentDetail } from './Environments'
const mocks = vi.hoisted(() => ({ call: vi.fn(), send: vi.fn() }))
vi.mock('./api', () => ({ labCall: mocks.call }))
vi.mock('@/features/chat/live', () => ({ sendMessage: mocks.send }))
vi.mock('./viewer', () => ({
  SceneView: ({ label }: { label: string }) => <div role="img" aria-label={label} />,
  useScene: () => ({ scene: null, loading: false }),
}))
const environment = {
  id: 'room',
  name: 'Room',
  kind: 'room',
  objects: 1,
  updated_at: '',
  source_type: 'text',
}
function mount(node: React.ReactNode) {
  return render(
    <QueryClientProvider
      client={
        new QueryClient({
          defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
        })
      }
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
    if (method === 'robotlab.env.list') return { environments: [environment] }
    if (method === 'robotlab.env.thumbnail') return { png: 'YWJj' }
    if (method === 'robotlab.env.get')
      return {
        environment: {
          ...environment,
          objects: [
            {
              id: 'ball',
              name: 'Ball',
              shape: 'sphere',
              size: [0.2],
              color: '#ff6600',
              dynamic: true,
            },
          ],
        },
      }
    return {}
  })
})
it.each(['en', 'es'])(
  'opens only text creation with the localized composer prefill (%s)',
  async (language) => {
    await i18n.changeLanguage(language)
    const draft = vi.fn()
    mount(<Environments onSelect={vi.fn()} onDraft={draft} />)
    await screen.findByText('Room')
    fireEvent.click(screen.getByRole('button', { name: language === 'es' ? /^Nuevo$/ : /^New$/ }))
    expect(draft).toHaveBeenCalledWith(
      language === 'es'
        ? 'Crea un ambiente de simulación a partir de esta descripción: '
        : 'Create a simulation environment from this description: ',
    )
    expect(document.querySelector('input[type=file]')).toBeNull()
    expect(mocks.send).not.toHaveBeenCalled()
  },
)
it('deletes only after confirmation and keeps conflicts visible', async () => {
  mocks.call.mockImplementation(async (method: string) => {
    if (method === 'robotlab.env.list') return { environments: [environment] }
    if (method === 'robotlab.env.thumbnail') return { png: '' }
    if (method === 'robotlab.env.delete') throw new Error('Used by live session')
    return {}
  })
  mount(<Environments onSelect={vi.fn()} onDraft={vi.fn()} />)
  fireEvent.click(await screen.findByRole('button', { name: 'Delete Room' }))
  expect(mocks.call).not.toHaveBeenCalledWith('robotlab.env.delete', expect.anything())
  fireEvent.click(screen.getByRole('button', { name: /^Delete$/ }))
  expect(await screen.findByRole('alert')).toHaveTextContent('Used by live session')
})
it('shows object dimensions, shape, color and dynamic state', async () => {
  const draft = vi.fn()
  mount(<EnvironmentDetail id="room" onBack={vi.fn()} onDraft={draft} />)
  expect(await screen.findByText('Ball')).toBeVisible()
  expect(screen.getByText('Shape: Sphere')).toBeVisible()
  expect(screen.getByText('Size (m): 0.2')).toBeVisible()
  expect(screen.getByRole('img', { name: 'Color: #ff6600' })).toBeVisible()
  expect(screen.getByText('Dynamic')).toBeVisible()
  fireEvent.click(screen.getByRole('button', { name: 'Edit with agent' }))
  expect(draft).toHaveBeenCalledWith('Edit environment "Room" (room): ')
})
