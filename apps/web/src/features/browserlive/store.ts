import { create } from 'zustand'
import type { BrowserActivityEvent, BrowserLiveState, BrowserSessionEvent } from './types'

export interface ConversationBrowser {
  session?: { id: string; run: string; state: 'active' | 'ended'; url?: string; title?: string }
  activity?: BrowserActivityEvent
  sessionAt: number
  seenSessions: string[]
  endedSessions: string[]
  dismissedSessions: string[]
}
export const emptyBrowser: ConversationBrowser = {
  sessionAt: 0,
  seenSessions: [],
  endedSessions: [],
  dismissedSessions: [],
}
export function applySession(
  state: ConversationBrowser,
  event: BrowserSessionEvent,
  conversation: string,
  at: number,
): ConversationBrowser {
  if (event.conversation_id !== conversation) return state
  const endedSessions =
    event.state === 'ended'
      ? [...new Set([...state.endedSessions, event.session_id])]
      : state.endedSessions
  const seenSessions = [...new Set([...state.seenSessions, event.session_id])]
  if (
    at < state.sessionAt ||
    (event.state === 'active' && endedSessions.includes(event.session_id))
  )
    return { ...state, endedSessions, seenSessions }
  return {
    ...state,
    session: {
      id: event.session_id,
      run: event.run_id,
      state: event.state,
      url: event.url ?? (state.session?.id === event.session_id ? state.session.url : undefined),
      title:
        event.title ?? (state.session?.id === event.session_id ? state.session.title : undefined),
    },
    activity: state.session?.id === event.session_id ? state.activity : undefined,
    sessionAt: at,
    endedSessions,
    seenSessions,
  }
}
export function applyActivity(
  state: ConversationBrowser,
  event: BrowserActivityEvent,
  conversation: string,
): ConversationBrowser {
  if (
    event.conversation_id !== conversation ||
    state.session?.id !== event.session_id ||
    state.session.state === 'ended' ||
    (state.activity && event.seq <= state.activity.seq)
  )
    return state
  return {
    ...state,
    activity: event,
    session: {
      ...state.session,
      url: event.url ?? state.session.url,
      title: event.title ?? state.session.title,
    },
  }
}
export function applyDismissal(state: ConversationBrowser): ConversationBrowser {
  if (state.session?.state !== 'active') return state
  return {
    ...state,
    dismissedSessions: [...new Set([...state.dismissedSessions, state.session.id])],
  }
}
export function applySnapshot(
  state: ConversationBrowser,
  snapshot: BrowserLiveState,
  conversation: string,
): ConversationBrowser {
  if (!snapshot.session) return { ...state, session: undefined, activity: undefined }
  // The snapshot is authoritative, but an ended session can never revive.
  let next = applySession(state, snapshot.session, conversation, state.sessionAt)
  if (
    next.session &&
    state.session?.id === next.session.id &&
    state.activity &&
    (!snapshot.activity || state.activity.seq > snapshot.activity.seq)
  ) {
    next = {
      ...next,
      session: { ...next.session, url: state.session.url, title: state.session.title },
    }
  }
  return snapshot.activity ? applyActivity(next, snapshot.activity, conversation) : next
}
export function shouldAutoOpen(
  state: ConversationBrowser,
  event: BrowserSessionEvent,
  at: string,
  replayed = false,
) {
  // A live delivery is fresh by definition. Comparing the daemon's timestamp with
  // this device's clock would make opening depend on clock skew between the two.
  return (
    !replayed &&
    event.state === 'active' &&
    Date.parse(at) >= state.sessionAt &&
    !state.seenSessions.includes(event.session_id) &&
    !state.dismissedSessions.includes(event.session_id) &&
    !state.endedSessions.includes(event.session_id)
  )
}
interface State {
  conversations: Record<string, ConversationBrowser>
  update: (
    conversation: string,
    apply: (previous: ConversationBrowser) => ConversationBrowser,
  ) => void
  dismiss: (conversation: string) => void
}
export const useBrowserLive = create<State>((set, get) => ({
  conversations: {},
  update: (conversation, apply) =>
    set((state) => ({
      conversations: {
        ...state.conversations,
        [conversation]: apply(state.conversations[conversation] ?? emptyBrowser),
      },
    })),
  dismiss: (conversation) => get().update(conversation, applyDismissal),
}))
