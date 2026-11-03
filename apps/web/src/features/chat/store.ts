import { create } from 'zustand'
import type { Message } from './types'
interface ChatState {
  activity: Record<string, string | undefined>
  setActivity: (id: string, value?: string) => void
  live: Record<string, Message[]>
  online: Record<string, boolean>
  connectedOnce: Record<string, boolean>
  older: Record<string, string | undefined>
  historyError: Record<string, string | undefined>
  historyLoaded: Record<string, boolean>
  historyCursor: Record<string, string | undefined>
  historyBefore: Record<string, string | undefined>
  startHistory: (id: string, before?: string) => void
  merge: (id: string, messages: Message[], history?: boolean | 'latest') => void
  patch: (id: string, messageId: string, patch: Partial<Message>) => void
  connection: (id: string, online: boolean) => void
  page: (id: string, before?: string, error?: string, latest?: string) => void
  typing: Record<string, boolean>
  setTyping: (id: string, value: boolean) => void
}
export const useChat = create<ChatState>((set) => ({
  activity: {},
  setActivity: (id, value) => set((s) => ({ activity: { ...s.activity, [id]: value } })),
  live: {},
  online: {},
  connectedOnce: {},
  older: {},
  historyError: {},
  historyLoaded: {},
  historyCursor: {},
  historyBefore: {},
  startHistory: (id, before) =>
    set((s) => ({
      historyError: { ...s.historyError, [id]: undefined },
      historyBefore: { ...s.historyBefore, [id]: before },
    })),
  merge: (id, messages, history = false) =>
    set((s) => {
      const byId = new Map((s.live[id] ?? []).map((message) => [message.id, message]))
      const existing = new Set(byId.keys())
      for (const message of messages) {
        // A persisted user echo can arrive before its receipt. Reconcile only an
        // in-flight optimistic message; never collapse two persisted messages.
        const pending =
          !history &&
          message.sender === 'user' &&
          !message.relayedBy &&
          !message.delivery &&
          !byId.has(message.id)
            ? [...byId.values()].find(
                (item) =>
                  item.sender === 'user' &&
                  item.delivery === 'sending' &&
                  item.text === message.text,
              )
            : undefined
        if (pending) byId.delete(pending.id)
        byId.set(message.id, {
          ...pending,
          ...byId.get(message.id),
          ...message,
          ...(pending ? { delivery: undefined, error: undefined } : {}),
        })
      }
      const order = [...byId.keys()]
      if (history) {
        // Insert history around shared anchors without moving known messages.
        // A latest page with no overlap appends; an older page prepends.
        const incoming = [...new Set(messages.map((message) => message.id))]
        const fresh = new Set(incoming.filter((key) => !existing.has(key)))
        order.splice(0, order.length, ...order.filter((key) => !fresh.has(key)))
        let position = history === 'latest' ? order.length : 0
        let pending: string[] = []
        for (const key of incoming) {
          const anchor = order.indexOf(key)
          if (anchor < 0) {
            pending.push(key)
          } else {
            order.splice(anchor, 0, ...pending)
            position = anchor + pending.length + 1
            pending = []
          }
        }
        order.splice(position, 0, ...pending)
      }
      const rows = order.map((key) => byId.get(key)!)
      return { live: { ...s.live, [id]: rows } }
    }),
  patch: (id, messageId, patch) =>
    set((s) => {
      const messages = (s.live[id] ?? []).map((message) =>
        message.id === messageId ? { ...message, ...patch } : message,
      )
      // A message event may precede the send receipt; keep the canonical copy only.
      const unique = new Map(messages.map((message) => [message.id, message]))
      return { live: { ...s.live, [id]: [...unique.values()] } }
    }),
  connection: (id, online) =>
    set((s) => ({
      online: { ...s.online, [id]: online },
      ...(online ? { connectedOnce: { ...s.connectedOnce, [id]: true } } : {}),
      ...(!online ? { typing: { ...s.typing, [id]: false } } : {}),
    })),
  page: (id, before, error, latest) =>
    set((s) => ({
      older: { ...s.older, [id]: before },
      historyError: { ...s.historyError, [id]: error },
      ...(!error ? { historyLoaded: { ...s.historyLoaded, [id]: true } } : {}),
      ...(latest ? { historyCursor: { ...s.historyCursor, [id]: latest } } : {}),
    })),
  typing: {},
  setTyping: (id, value) => set((s) => ({ typing: { ...s.typing, [id]: value } })),
}))
