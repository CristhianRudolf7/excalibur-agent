import { beforeEach, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import i18n from '@/lib/i18n'
import { RobotLabSettings } from '@/features/settings/RobotLabSettings'
const call = vi.hoisted(() => vi.fn())
vi.mock('./api', () => ({ labCall: call }))
beforeEach(async () => {
  cleanup()
  vi.clearAllMocks()
  await i18n.changeLanguage('en')
  call.mockImplementation(async (method: string, params: unknown) =>
    method === 'robotlab.settings.get' ? { max_attempts: 40, sim_speed: 1 } : params,
  )
})
function mount() {
  return render(
    <QueryClientProvider
      client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
    >
      <RobotLabSettings />
    </QueryClientProvider>,
  )
}
it('loads current settings and saves a bounded update', async () => {
  mount()
  expect(await screen.findByLabelText('Maximum attempts')).toHaveValue(40)
  expect(screen.getByLabelText('Simulation speed')).toHaveValue(1)
  fireEvent.change(screen.getByLabelText('Maximum attempts'), { target: { value: '35' } })
  fireEvent.change(screen.getByLabelText('Simulation speed'), { target: { value: '0.5' } })
  fireEvent.click(screen.getByRole('button', { name: 'Save' }))
  await waitFor(() =>
    expect(call).toHaveBeenCalledWith('robotlab.settings.set', {
      max_attempts: 35,
      sim_speed: 0.5,
    }),
  )
  expect(await screen.findByRole('status')).toHaveTextContent('Saved')
})
it('rejects invalid settings and reports backend failures', async () => {
  const view = mount()
  await screen.findByLabelText('Maximum attempts')
  fireEvent.change(screen.getByLabelText('Maximum attempts'), { target: { value: '101' } })
  fireEvent.submit(view.container.querySelector('form')!)
  expect(screen.getByRole('alert')).toHaveTextContent('Use 1–100 attempts')
  expect(call).not.toHaveBeenCalledWith('robotlab.settings.set', expect.anything())
  call.mockRejectedValueOnce(new Error('Runtime unavailable'))
  fireEvent.change(screen.getByLabelText('Maximum attempts'), { target: { value: '10' } })
  fireEvent.submit(view.container.querySelector('form')!)
  expect(await screen.findByRole('alert')).toHaveTextContent('Runtime unavailable')
})

it.each(['en', 'es'])('defaults missing max attempts to 40 (%s)', async (language) => {
  await i18n.changeLanguage(language)
  call.mockResolvedValueOnce({ sim_speed: 1 })
  mount()
  expect(
    await screen.findByLabelText(language === 'es' ? 'Máximo de intentos' : 'Maximum attempts'),
  ).toHaveValue(40)
})
it('preserves an explicitly saved attempt limit', async () => {
  call.mockResolvedValueOnce({ max_attempts: 12, sim_speed: 1 })
  mount()
  expect(await screen.findByLabelText('Maximum attempts')).toHaveValue(12)
})
