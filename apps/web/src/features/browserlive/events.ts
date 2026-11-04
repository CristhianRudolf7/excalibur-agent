import { useEffect, useEffectEvent } from 'react'
import { useCall } from '@/features/chat/voice/callStore'
import { conversationId } from '@/features/chat/live'
import { openPanelView } from '@/features/files/open'
import { rpc } from '@/lib/rpc/client'
import { subscribeEvents } from '@/lib/rpc/events'
import {
  applyActivity,
  applySession,
  applySnapshot,
  emptyBrowser,
  shouldAutoOpen,
  useBrowserLive,
} from './store'
import type { BrowserSessionEvent, BrowserActivityEvent } from './types'

export function useBrowserLiveEvents(agent?: string, onCredentials?: () => void) {
  const credentials = useEffectEvent(() => onCredentials?.())
  const conversation = agent ? conversationId(agent) : undefined
  useEffect(() => {
    if (!conversation) return
    let stopped = false
    let revision = 0
    const current = () => useBrowserLive.getState().conversations[conversation] ?? emptyBrowser
    const update = useBrowserLive.getState().update
    const restore = async (candidate?: BrowserSessionEvent) => {
      const requested = ++revision
      try {
        const snapshot = await rpc.call('browser.live_state', { conversation_id: conversation })
        if (stopped || requested !== revision) return
        update(conversation, (previous) => applySnapshot(previous, snapshot, conversation))
        const state = current()
        if (
          candidate &&
          snapshot.session?.conversation_id === conversation &&
          snapshot.session.session_id === candidate.session_id &&
          state.session?.state === 'active' &&
          !state.dismissedSessions.includes(candidate.session_id)
        ) {
          useCall.getState().hide()
          openPanelView('browser')
        }
      } catch {
        /* The next ready or session event retries the state read. */
      }
    }
    const stop = subscribeEvents({
      types: ['browser.session', 'browser.activity', 'credentials.requested'],
      ready: () => {
        void restore()
      },
      offline: () => {
        ++revision
      },
      event: (event, replayed) => {
        const payload = event.payload
        if (!('conversation_id' in payload) || payload.conversation_id !== conversation) return
        if (event.type === 'credentials.requested') {
          useCall.getState().hide()
          credentials()
          return
        }
        if (event.type === 'browser.session') {
          const session = payload as BrowserSessionEvent
          const auto = shouldAutoOpen(current(), session, event.at, replayed)
          ++revision
          update(conversation, (previous) =>
            applySession(previous, session, conversation, Date.parse(event.at) || 0),
          )
          if (auto) void restore(session)
        } else if (event.type === 'browser.activity') {
          update(conversation, (previous) =>
            applyActivity(previous, payload as BrowserActivityEvent, conversation),
          )
        }
      },
    })
    return () => {
      stopped = true
      stop()
    }
  }, [conversation])
}
