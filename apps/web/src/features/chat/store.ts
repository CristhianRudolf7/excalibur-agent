import { create } from 'zustand'
import type { Message } from './types'
interface ChatState {
  activity: Record<string, string | undefined>
  setActivity: (id: string, value?: string) => void
  live: Record<string, Message[]>
  online: Record<string, boolean>
  older: Record<string, string | undefined>
  historyError: Record<string, string | undefined>
  merge: (id: string, messages: Message[], history?: boolean) => void
  patch: (id: string, messageId: string, patch: Partial<Message>) => void
  connection: (id: string, online: boolean) => void
  page: (id: string, before?: string, error?: string) => void
  typing: Record<string, boolean>
  setTyping: (id: string, value: boolean) => void
}
export const useChat = create<ChatState>((set) => ({
  activity: {},
  setActivity: (id, value) => set((s) => ({ activity: { ...s.activity, [id]: value } })),
  live: {},
  online: {},
  older: {},
  historyError: {},
  merge: (id, messages, history = false) =>
    set((s) => {
      const byId = new Map((s.live[id] ?? []).map((message) => [message.id, message]))
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
      const incoming = new Set(messages.map((message) => message.id))
      const rows = history
        ? [
            ...messages.map((message) => byId.get(message.id)!),
            ...[...byId.values()].filter((message) => !incoming.has(message.id)),
          ]
        : [...byId.values()]
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
      ...(!online ? { typing: { ...s.typing, [id]: false } } : {}),
    })),
  page: (id, before, error) =>
    set((s) => ({
      older: { ...s.older, [id]: before },
      historyError: { ...s.historyError, [id]: error },
    })),
  typing: {},
  setTyping: (id, value) => set((s) => ({ typing: { ...s.typing, [id]: value } })),
}))
