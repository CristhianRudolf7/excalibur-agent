import { act, cleanup, renderHook, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { rpc } from '@/lib/rpc/client'
import { subscribeEvents } from '@/lib/rpc/events'
import { openPanelView } from '@/features/files/open'
import type { ConversationEvent } from '@/lib/rpc/contract'
import { useBrowserLiveEvents } from './events'
import { useBrowserLive } from './store'
import type { BrowserSessionEvent } from './types'
vi.mock('@/lib/rpc/events', () => ({ subscribeEvents: vi.fn(() => vi.fn()) }))
vi.mock('@/features/files/open', () => ({ openPanelView: vi.fn() }))
vi.mock('@/features/chat/live', () => ({ conversationId: (id: string) => `cnv_${id}` }))
const session: BrowserSessionEvent = {
  conversation_id: 'cnv_agent',
  session_id: 's',
  run_id: 'r',
  state: 'active',
}
const event = (payload: unknown, type = 'browser.session', at = new Date().toISOString()) =>
  ({ payload, type, at }) as ConversationEvent
function setup(callback?: () => void) {
  const hook = renderHook(() => useBrowserLiveEvents('agent', callback))
  const subscription = vi.mocked(subscribeEvents).mock.calls.at(-1)![0]
  return { hook, subscription }
}
beforeEach(() => {
  useBrowserLive.setState({ conversations: {} })
  vi.mocked(subscribeEvents).mockClear()
  vi.mocked(openPanelView).mockClear()
})
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})
it('restores active state after ready without opening; recent replay stays closed too', async () => {
  vi.spyOn(rpc, 'call').mockResolvedValue({ session })
  const { subscription } = setup()
  act(() => {
    subscription.event(event(session), true)
    subscription.ready()
  })
  await waitFor(() =>
    expect(rpc.call).toHaveBeenCalledWith('browser.live_state', { conversation_id: 'cnv_agent' }),
  )
  expect(useBrowserLive.getState().conversations.cnv_agent?.session?.state).toBe('active')
  expect(openPanelView).not.toHaveBeenCalled()
})
it('only opens a fresh session when the authoritative state agrees, once per session', async () => {
  vi.spyOn(rpc, 'call').mockResolvedValue({ session })
  const { subscription } = setup()
  act(() => subscription.event(event(session)))
  await waitFor(() => expect(openPanelView).toHaveBeenCalledWith('browser'))
  act(() => subscription.event(event(session)))
  expect(openPanelView).toHaveBeenCalledTimes(1)
  act(() => useBrowserLive.getState().dismiss('cnv_agent'))
  act(() => subscription.event(event(session)))
  expect(openPanelView).toHaveBeenCalledTimes(1)
})
it('ignores replayed, foreign and ended events and refuses auto-open on a mismatched snapshot', async () => {
  vi.spyOn(rpc, 'call').mockResolvedValue({ session: { ...session, session_id: 'different' } })
  const { subscription } = setup()
  act(() => subscription.event(event({ ...session, conversation_id: 'foreign' })))
  expect(rpc.call).not.toHaveBeenCalled()
  act(() =>
    subscription.event(
      event(session, 'browser.session', new Date(Date.now() - 20_000).toISOString()),
      true,
    ),
  )
  expect(rpc.call).not.toHaveBeenCalled()
  act(() => subscription.event(event({ ...session, session_id: 'next' })))
  await waitFor(() => expect(rpc.call).toHaveBeenCalledOnce())
  expect(openPanelView).not.toHaveBeenCalled()
  act(() => subscription.event(event({ ...session, state: 'ended' }), true))
  expect(openPanelView).not.toHaveBeenCalled()
})
it('drops obsolete state responses and does not open after unmount', async () => {
  let resolve!: (value: { session: BrowserSessionEvent }) => void
  vi.spyOn(rpc, 'call').mockImplementation(
    () =>
      new Promise((done) => {
        resolve = done
      }),
  )
  const { hook, subscription } = setup()
  act(() => subscription.event(event(session)))
  act(() => subscription.event(event({ ...session, state: 'ended' })))
  await act(async () => resolve({ session }))
  expect(useBrowserLive.getState().conversations.cnv_agent?.session?.state).toBe('ended')
  expect(openPanelView).not.toHaveBeenCalled()
  act(() => subscription.event(event({ ...session, session_id: 'next' })))
  hook.unmount()
  await act(async () => resolve({ session: { ...session, session_id: 'next' } }))
  expect(openPanelView).not.toHaveBeenCalled()
})
it('closes for credential requests only in the on-screen conversation', () => {
  const close = vi.fn(),
    { subscription } = setup(close)
  act(() => subscription.event(event({ conversation_id: 'foreign' }, 'credentials.requested')))
  expect(close).not.toHaveBeenCalled()
  act(() => subscription.event(event({ conversation_id: 'cnv_agent' }, 'credentials.requested')))
  expect(close).toHaveBeenCalledOnce()
})
it('opens a live event regardless of the clock difference with the daemon', async () => {
  vi.spyOn(rpc, 'call').mockResolvedValue({ session })
  const { subscription } = setup()
  const skewed = new Date(Date.now() + 90_000).toISOString()
  act(() => subscription.event(event(session, 'browser.session', skewed)))
  await waitFor(() => expect(openPanelView).toHaveBeenCalledOnce())
})
