import { createRef } from 'react'
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import i18n from '@/lib/i18n'
import { openPanelView } from '@/features/files/open'
import { BrowserLivePanel } from './BrowserLivePanel'
import { BrowserReopen } from './BrowserReopen'
import { useBrowserFrames } from './useBrowserFrames'
import { applyActivity, applySession, emptyBrowser, useBrowserLive } from './store'
import type { BrowserActivityEvent, BrowserSessionEvent } from './types'
vi.mock('./useBrowserFrames', () => ({ useBrowserFrames: vi.fn() }))
vi.mock('@/features/files/open', () => ({ openPanelView: vi.fn() }))
const session: BrowserSessionEvent = {
  conversation_id: 'c',
  session_id: 's',
  run_id: 'r',
  state: 'active',
  url: 'https://example.test/path',
  title: 'Local page',
}
const activity: BrowserActivityEvent = {
  ...session,
  seq: 1,
  phase: 'start',
  kind: 'click',
  target: 'Continue',
  box: { x: 100, y: 50, width: 200, height: 100 },
}
const send = (action: BrowserActivityEvent) =>
  useBrowserLive.getState().update('c', (previous) => applyActivity(previous, action, 'c'))
beforeEach(async () => {
  await i18n.changeLanguage('en')
  vi.useFakeTimers()
  useBrowserLive.setState({ conversations: { c: applySession(emptyBrowser, session, 'c', 1) } })
  vi.mocked(openPanelView).mockClear()
  vi.mocked(useBrowserFrames).mockReturnValue({
    canvas: createRef<HTMLCanvasElement>(),
    frame: { session: 's', header: { seq: 1, tab_id: 't', width: 800, height: 400, at: 1 } },
  })
})
afterEach(() => {
  cleanup()
  vi.useRealTimers()
})
it('renders URL, title, label and percentage highlight with a noninteractive stage', () => {
  send(activity)
  const { container } = render(<BrowserLivePanel conversation="c" />)
  expect(screen.getByTitle('https://example.test/path')).toHaveTextContent('example.test/path')
  expect(screen.getByText('Local page')).toBeVisible()
  expect(screen.getByText("Click on 'Continue'")).toBeVisible()
  expect(screen.getByTestId('browser-highlight')).toHaveStyle({
    left: '12.5%',
    top: '12.5%',
    width: '25%',
    height: '25%',
  })
  const stage = container.querySelector('.browser-live-stage')!
  expect(stage).toHaveStyle({ aspectRatio: '800 / 400' })
  expect(
    stage.querySelectorAll('button, input, textarea, select, a, [tabindex], [contenteditable]'),
  ).toHaveLength(0)
  expect(screen.getByRole('img')).toHaveAttribute('draggable', 'false')
  fireEvent.click(stage)
  expect(openPanelView).not.toHaveBeenCalled()
})
it('keeps labels for 700ms and highlights for 500ms after done; page does not replace the label', () => {
  send({ ...activity, kind: 'wait' })
  render(<BrowserLivePanel conversation="c" />)
  act(() => {
    vi.advanceTimersByTime(100)
    send({ ...activity, seq: 2, kind: 'read' })
  })
  expect(screen.getByText('Waiting for the page')).toBeVisible()
  act(() => send({ ...activity, seq: 3, kind: 'read', phase: 'done' }))
  act(() => vi.advanceTimersByTime(499))
  expect(screen.getByTestId('browser-highlight')).toBeVisible()
  act(() => vi.advanceTimersByTime(1))
  expect(screen.queryByTestId('browser-highlight')).not.toBeInTheDocument()
  act(() => vi.advanceTimersByTime(100))
  expect(screen.getByText('Reading the page')).toBeVisible()
  act(() =>
    send({ ...activity, seq: 4, kind: 'page', phase: 'done', url: 'https://example.test/next' }),
  )
  expect(screen.getByText('Reading the page')).toBeVisible()
  expect(screen.getByTitle('https://example.test/next')).toBeVisible()
})
it('shows a connecting skeleton and an ended notice while retaining the last frame', () => {
  vi.mocked(useBrowserFrames).mockReturnValueOnce({
    canvas: createRef<HTMLCanvasElement>(),
    frame: undefined,
  })
  const view = render(<BrowserLivePanel conversation="c" />)
  expect(screen.getByText('Connecting…')).toBeVisible()
  act(() =>
    useBrowserLive
      .getState()
      .update('c', (previous) => applySession(previous, { ...session, state: 'ended' }, 'c', 2)),
  )
  view.rerender(<BrowserLivePanel conversation="c" />)
  expect(screen.getByText('Ended')).toBeVisible()
  expect(screen.getByText('The browser session ended')).toBeVisible()
  expect(view.container.querySelector('.browser-live-ended')).toBeInTheDocument()
  expect(screen.getByRole('img')).toBeVisible()
})
it('only shows the reopen control for an active session outside the browser view', () => {
  const view = render(<BrowserReopen conversation="c" visible={false} />)
  fireEvent.click(screen.getByRole('button', { name: 'Show live browser' }))
  expect(openPanelView).toHaveBeenCalledWith('browser')
  view.rerender(<BrowserReopen conversation="c" visible />)
  expect(screen.queryByRole('button')).not.toBeInTheDocument()
  view.rerender(<BrowserReopen conversation="other" visible={false} />)
  expect(screen.queryByRole('button')).not.toBeInTheDocument()
  act(() =>
    useBrowserLive
      .getState()
      .update('c', (previous) => applySession(previous, { ...session, state: 'ended' }, 'c', 2)),
  )
  view.rerender(<BrowserReopen conversation="c" visible={false} />)
  expect(screen.queryByRole('button')).not.toBeInTheDocument()
})
