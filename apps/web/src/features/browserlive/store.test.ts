import { expect, it } from 'vitest'
import {
  applySession,
  applyActivity,
  applyDismissal,
  applySnapshot,
  emptyBrowser,
  shouldAutoOpen,
} from './store'
import type { BrowserSessionEvent, BrowserActivityEvent } from './types'
export const session: BrowserSessionEvent = {
  conversation_id: 'conversation',
  run_id: 'run',
  session_id: 'session',
  state: 'active',
  url: 'https://example.test/path',
  title: 'Local page',
}
export const activity: BrowserActivityEvent = {
  ...session,
  seq: 1,
  phase: 'start',
  kind: 'click',
  target: 'Continue',
  box: { x: 100, y: 50, width: 200, height: 100 },
}
it('filters conversations and session IDs and orders activity by sequence', () => {
  expect(applySession(emptyBrowser, session, 'other', 1)).toBe(emptyBrowser)
  const opened = applySession(emptyBrowser, session, 'conversation', 1)
  expect(applyActivity(opened, activity, 'other')).toBe(opened)
  expect(applyActivity(opened, { ...activity, session_id: 'other' }, 'conversation')).toBe(opened)
  const latest = applyActivity(
    opened,
    { ...activity, seq: 5, url: 'https://example.test/new' },
    'conversation',
  )
  expect(applyActivity(latest, activity, 'conversation')).toBe(latest)
  expect(applyActivity(latest, { ...activity, seq: 5 }, 'conversation')).toBe(latest)
  expect(latest.session?.url).toBe('https://example.test/new')
})
it('never revives ended sessions and ignores sessions from older replay', () => {
  const ended = applySession(emptyBrowser, { ...session, state: 'ended' }, 'conversation', 10)
  expect(applySession(ended, session, 'conversation', 20).session?.state).toBe('ended')
  expect(applyActivity(ended, activity, 'conversation')).toBe(ended)
  const next = applySession(ended, { ...session, session_id: 'next' }, 'conversation', 30)
  expect(applySession(next, session, 'conversation', 5).session?.id).toBe('next')
  expect(applySession(next, { ...session, state: 'ended' }, 'conversation', 10).session?.id).toBe(
    'next',
  )
})
it('allows auto-open once for a fresh live event and never on replayed, ended or dismissed events', () => {
  const now = Date.now(),
    at = new Date(now).toISOString()
  expect(shouldAutoOpen(emptyBrowser, session, at)).toBe(true)
  expect(shouldAutoOpen(emptyBrowser, session, at, true)).toBe(false)
  expect(shouldAutoOpen(emptyBrowser, session, 'invalid')).toBe(false)
  expect(shouldAutoOpen(emptyBrowser, { ...session, state: 'ended' }, at)).toBe(false)
  const seen = applySession(emptyBrowser, session, 'conversation', now)
  expect(shouldAutoOpen(seen, session, at)).toBe(false)
  const dismissed = applyDismissal(seen)
  expect(dismissed.dismissedSessions).toContain('session')
  expect(shouldAutoOpen(dismissed, session, at)).toBe(false)
  expect(shouldAutoOpen(dismissed, { ...session, session_id: 'next' }, at)).toBe(true)
})
it('restores active state as already seen and can clear absent state without persistence', () => {
  const restored = applySnapshot(emptyBrowser, { session, activity }, 'conversation')
  expect(restored.activity).toEqual(activity)
  expect(restored.seenSessions).toEqual(['session'])
  expect(shouldAutoOpen(restored, session, new Date().toISOString())).toBe(false)
  expect(applySnapshot(restored, {}, 'conversation').session).toBeUndefined()
  expect(applyDismissal(emptyBrowser)).toBe(emptyBrowser)
})
it('preserves page metadata on end and does not roll back activities received during a state read', () => {
  const opened = applySession(emptyBrowser, session, 'conversation', 1)
  const latest = applyActivity(
    opened,
    { ...activity, seq: 5, url: 'https://example.test/new', title: 'New title' },
    'conversation',
  )
  const snapshot = applySnapshot(latest, { session, activity }, 'conversation')
  expect(snapshot.activity?.seq).toBe(5)
  expect(snapshot.session?.url).toBe('https://example.test/new')
  const ended = applySession(
    latest,
    { ...session, state: 'ended', url: undefined, title: undefined },
    'conversation',
    2,
  )
  expect(ended.session?.url).toBe('https://example.test/new')
  expect(ended.session?.title).toBe('New title')
})
