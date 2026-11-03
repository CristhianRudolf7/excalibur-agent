import '@/lib/i18n'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { MappingEditor } from './MappingEditor'
const call = vi.hoisted(() => vi.fn())
vi.mock('./api', () => ({ robotlabCall: call }))
vi.mock('@/features/files/CodeEditor', () => ({}))
vi.mock('@monaco-editor/react', () => ({
  default: ({ value, onChange }: { value: string; onChange: (v: string) => void }) => (
    <textarea aria-label="map_pose" value={value} onChange={(e) => onChange(e.target.value)} />
  ),
}))
afterEach(() => {
  cleanup()
  vi.resetAllMocks()
})
const defaultCode = 'def map_pose(pose, state):\n    return {}'
const customCode = 'def map_pose(pose, state):\n    return {"joint": 0}'
it('reads the current mapping on open, after save/reset, and on reopen', async () => {
  let code = customCode,
    source = 'custom'
  call.mockImplementation(async (method: string, params: { code?: string | null }) => {
    if (method === 'robotlab.teleop.get') return { source, code }
    source = params.code === null ? 'default' : 'custom'
    code = params.code ?? defaultCode
    return { ok: true }
  })
  render(<MappingEditor session="s" />)
  expect(call).not.toHaveBeenCalled()
  fireEvent.click(screen.getByRole('button', { name: /View\/edit|Ver\/editar/ }))
  expect(await screen.findByRole('textbox', { name: 'map_pose' })).toHaveValue(customCode)
  expect(call).toHaveBeenCalledWith('robotlab.teleop.get', { session: 's' })
  fireEvent.change(screen.getByRole('textbox'), { target: { value: customCode + '\n# edited' } })
  fireEvent.click(screen.getByRole('button', { name: /Save mapping|Guardar mapeo/ }))
  await waitFor(() => expect(screen.getByRole('textbox')).toHaveValue(customCode + '\n# edited'))
  expect(call).toHaveBeenLastCalledWith('robotlab.teleop.get', { session: 's' })
  fireEvent.click(screen.getByRole('button', { name: /Use scenario default|Usar predeterminado/ }))
  await waitFor(() => expect(screen.getByRole('textbox')).toHaveValue(defaultCode))
  expect(screen.getByText(/Scenario default mapping|Mapeo predeterminado/)).toBeVisible()
  fireEvent.click(screen.getByRole('button', { name: /Close editor|Cerrar editor/ }))
  code = customCode
  fireEvent.click(screen.getByRole('button', { name: /View\/edit|Ver\/editar/ }))
  await waitFor(() => expect(screen.getByRole('textbox')).toHaveValue(customCode))
  expect(call.mock.calls.filter(([method]) => method === 'robotlab.teleop.get')).toHaveLength(4)
})
it('shows validation errors and preserves the edited code', async () => {
  call.mockImplementation(async (method: string) =>
    method === 'robotlab.teleop.get'
      ? { source: 'custom', code: 'bad code' }
      : { ok: false, errors: ['map_pose must be defined'] },
  )
  render(<MappingEditor session="s" />)
  fireEvent.click(screen.getByRole('button', { name: /View\/edit|Ver\/editar/ }))
  await screen.findByRole('textbox')
  fireEvent.click(screen.getByRole('button', { name: /Save mapping|Guardar mapeo/ }))
  expect(await screen.findByRole('alert')).toHaveTextContent('map_pose must be defined')
  expect(screen.getByRole('textbox')).toHaveValue('bad code')
})
it('offers an error fallback and retries reading on reopen', async () => {
  call
    .mockRejectedValueOnce(new Error('Mapping unavailable'))
    .mockResolvedValue({ source: 'default', code: defaultCode })
  render(<MappingEditor session="s" />)
  expect(
    screen.queryByText(/Current mapping code|El servicio no proporciona/),
  ).not.toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: /View\/edit|Ver\/editar/ }))
  expect(await screen.findByRole('alert')).toHaveTextContent('Mapping unavailable')
  fireEvent.click(screen.getByRole('button', { name: /Close editor|Cerrar editor/ }))
  fireEvent.click(screen.getByRole('button', { name: /View\/edit|Ver\/editar/ }))
  await waitFor(() => expect(screen.getByRole('textbox')).toHaveValue(defaultCode))
  expect(screen.queryByRole('alert')).not.toBeInTheDocument()
})
it('does not let an old session read replace a new session mapping', async () => {
  let resolve!: (value: unknown) => void
  call
    .mockReturnValueOnce(
      new Promise((done) => {
        resolve = done
      }),
    )
    .mockResolvedValue({ source: 'default', code: defaultCode })
  const view = render(<MappingEditor session="old" />)
  fireEvent.click(screen.getByRole('button', { name: /View\/edit|Ver\/editar/ }))
  view.rerender(<MappingEditor session="new" />)
  fireEvent.click(screen.getByRole('button', { name: /View\/edit|Ver\/editar/ }))
  await waitFor(() => expect(screen.getByRole('textbox')).toHaveValue(defaultCode))
  resolve({ source: 'custom', code: customCode })
  await waitFor(() => expect(screen.getByRole('textbox')).toHaveValue(defaultCode))
})
