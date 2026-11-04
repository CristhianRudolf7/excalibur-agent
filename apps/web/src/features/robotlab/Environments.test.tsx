import { beforeEach, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import '@/lib/i18n'
import i18n from '@/lib/i18n'
import { Environments, EnvironmentDetail, MediaDialog } from './Environments'
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
    if (method === 'robotlab.media.upload') return { media_id: 'media-1' }
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
it('opens text creation with the existing composer prefill', async () => {
  const draft = vi.fn()
  mount(<Environments onSelect={vi.fn()} onDraft={draft} />)
  await screen.findByText('Room')
  fireEvent.click(screen.getByRole('button', { name: /^New$/ }))
  fireEvent.click(screen.getByRole('button', { name: 'From chat' }))
  expect(draft).toHaveBeenCalledWith('Create a simulation environment from this description: ')
})
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
it.each(['en', 'es'])('uploads media and sends a localized instruction (%s)', async (language) => {
  await i18n.changeLanguage(language)
  const close = vi.fn(),
    agent = vi.fn()
  mount(<MediaDialog onClose={close} onAgent={agent} />)
  fireEvent.change(
    screen.getByLabelText(
      language === 'es' ? 'Imagen o video (máximo 32 MB)' : 'Image or video (up to 32 MB)',
    ),
    {
      target: { files: [new File(['abc'], 'room.png', { type: 'image/png' })] },
    },
  )
  fireEvent.change(
    screen.getByLabelText(language === 'es' ? 'Especificación opcional' : 'Optional specification'),
    {
      target: { value: 'Keep the desk' },
    },
  )
  fireEvent.click(
    screen.getByRole('button', { name: language === 'es' ? 'Enviar al agente' : 'Send to agent' }),
  )
  await waitFor(() =>
    expect(mocks.send).toHaveBeenCalledWith(
      'excalibur',
      language === 'es'
        ? 'Crea un ambiente a partir del archivo "room.png" (media media-1). Especificación: Keep the desk'
        : 'Create an environment from the file "room.png" (media media-1). Specification: Keep the desk',
    ),
  )
  expect(mocks.call).toHaveBeenCalledWith(
    'robotlab.media.upload',
    { name: 'room.png', mime: 'image/png', data: 'YWJj', spec: 'Keep the desk' },
    expect.anything(),
  )
  expect(agent).toHaveBeenCalled()
  expect(close).toHaveBeenCalled()
})
it('rejects unsupported media before sending any upload', async () => {
  mount(<MediaDialog onClose={vi.fn()} onAgent={vi.fn()} />)
  fireEvent.change(screen.getByLabelText('Image or video (up to 32 MB)'), {
    target: { files: [new File(['abc'], 'bad.txt', { type: 'text/plain' })] },
  })
  expect(screen.getByRole('alert')).toHaveTextContent('Choose an image or video up to 32 MB.')
  expect(screen.getByRole('button', { name: 'Send to agent' })).toBeDisabled()
  expect(mocks.call).not.toHaveBeenCalled()
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
