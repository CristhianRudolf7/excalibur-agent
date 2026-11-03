import { StrictMode, type ReactNode } from 'react'
import { afterEach, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import '@/lib/i18n'
import i18n from '@/lib/i18n'
import { rpc } from '@/lib/rpc/client'
import type { Principal } from '@/lib/rpc/contract'
import { TimeZone } from './TimeZone'
import { BrowserTimeZoneDetection } from './time-zone-api'
import { browserTimeZone } from './time-zone'

const clients: QueryClient[] = []
afterEach(() => {
  cleanup()
  clients.forEach((client) => client.clear())
  clients.length = 0
  vi.restoreAllMocks()
})
function setup(source: Principal['timezone_source'] = 'default', zone = 'UTC') {
  let principal: Principal = {
    id: 'local',
    name: 'Example',
    timezone: zone,
    timezone_source: source,
    language: 'en',
  }
  const call = vi.spyOn(rpc, 'call').mockImplementation(async (method, params) => {
    if (method === 'principal.update') {
      const update = params as {
        timezone: string
        timezone_source: Principal['timezone_source']
        enable_auto?: boolean
      }
      if (
        update.timezone_source !== 'auto' ||
        principal.timezone_source !== 'manual' ||
        update.enable_auto
      )
        principal = { ...principal, ...update }
    }
    return principal
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  clients.push(client)
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  )
  return { call, client, wrapper }
}
it('defaults detection on, saves a manual choice and re-applies the browser zone explicitly', async () => {
  await i18n.changeLanguage('en')
  const { wrapper, call, client } = setup()
  render(<TimeZone />, { wrapper })
  const toggle = await screen.findByRole('switch', { name: 'Detect automatically' })
  expect(toggle).toBeChecked()
  fireEvent.click(screen.getByRole('button', { name: 'Time zone' }))
  const search = screen.getByRole('textbox', { name: 'Search time zones' })
  fireEvent.change(search, { target: { value: 'no such time zone' } })
  expect(screen.getByText('No time zones found.')).toBeVisible()
  fireEvent.change(search, { target: { value: 'lima' } })
  fireEvent.click(screen.getByRole('button', { name: 'America/Lima' }))
  await waitFor(() => expect(toggle).not.toBeChecked())
  expect(call).toHaveBeenCalledWith('principal.update', {
    timezone: 'America/Lima',
    timezone_source: 'manual',
  })
  expect(screen.getByLabelText('Current local time')).toHaveTextContent('GMT-05:00')
  expect(client.getQueryData(['principal'])).toMatchObject({
    timezone: 'America/Lima',
    timezone_source: 'manual',
  })
  fireEvent.click(toggle)
  await waitFor(() => expect(toggle).toBeChecked())
  expect(call).toHaveBeenCalledWith('principal.update', {
    timezone: browserTimeZone(),
    timezone_source: 'auto',
    enable_auto: true,
  })
})
it('can disable detection without changing the chosen zone and shows save failures', async () => {
  await i18n.changeLanguage('en')
  const { wrapper, call } = setup('auto', 'America/Lima')
  render(<TimeZone />, { wrapper })
  const toggle = await screen.findByRole('switch')
  call.mockRejectedValueOnce(new Error('offline'))
  fireEvent.click(toggle)
  expect(await screen.findByRole('alert')).toHaveTextContent('Could not save the time zone.')
  expect(toggle).toBeChecked()
  fireEvent.click(toggle)
  await waitFor(() => expect(toggle).not.toBeChecked())
  expect(call).toHaveBeenLastCalledWith('principal.update', {
    timezone: 'America/Lima',
    timezone_source: 'manual',
  })
})
it('reports the browser zone once across StrictMode, remount and focus without replacing a manual profile', async () => {
  await i18n.changeLanguage('es')
  const { wrapper, call, client } = setup('manual', 'America/Lima')
  const first = render(
    <StrictMode>
      <BrowserTimeZoneDetection />
      <TimeZone />
    </StrictMode>,
    { wrapper },
  )
  const toggle = await screen.findByRole('switch', { name: 'Detectar automáticamente' })
  await waitFor(() =>
    expect(call.mock.calls.filter(([method]) => method === 'principal.update')).toHaveLength(1),
  )
  await waitFor(() => expect(client.getQueryState(['browser-time-zone'])?.status).toBe('success'))
  expect(toggle).not.toBeChecked()
  expect(screen.getByRole('button', { name: 'Zona horaria' })).toHaveTextContent('America/Lima')
  first.unmount()
  render(<BrowserTimeZoneDetection />, { wrapper })
  fireEvent.focus(window)
  expect(call.mock.calls.filter(([method]) => method === 'principal.update')).toHaveLength(1)
  expect(call).toHaveBeenCalledWith('principal.update', {
    timezone: browserTimeZone(),
    timezone_source: 'auto',
  })
})
it('applies automatic detection from a default profile and refreshes the shared profile', async () => {
  const { wrapper, client } = setup()
  render(
    <>
      <BrowserTimeZoneDetection />
      <TimeZone />
    </>,
    { wrapper },
  )
  await waitFor(() =>
    expect(client.getQueryData(['principal'])).toMatchObject({
      timezone: browserTimeZone(),
      timezone_source: 'auto',
    }),
  )
})
