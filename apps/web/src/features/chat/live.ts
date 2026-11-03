import { useAgents } from '@/features/agents/store'
import type { ClassroomContext } from '@/lib/rpc/classroom'
import i18n from '@/lib/i18n'
import { rpc, RpcError, TransportError } from '@/lib/rpc/client'
import { subscribeEvents } from '@/lib/rpc/events'
import type { ConversationEvent, ConversationMessage, DocumentInfo } from '@/lib/rpc/contract'
import { documentUpdated } from '@/features/library/store'
import { useChat } from './store'
import type { Message } from './types'

export const liveChatEnabled = import.meta.env.EXCALIBUR_WEB_CHAT_LIVE !== '0'
// The default identity retains the installation's existing history.
export const conversationId = (agent: string) =>
  useAgents.getState().agents.find((item) => item.id === agent)?.conversation_id ??
  (agent === 'excalibur' ? 'cnv_web_excalibur' : `cnv_agent_${agent}`)
export const chatStateKey = (agent: string, target?: string) =>
  target && target !== conversationId(agent) ? `${agent}:${target}` : agent
const pageSize = 50
const unavailable = (error: unknown) =>
  error instanceof TransportError || (error instanceof RpcError && error.code === -32601)
export function fromWire(message: ConversationMessage): Message {
  return {
    id: message.id,
    sender: message.role === 'user' || message.relayed_by ? 'user' : 'agent',
    relayedBy: message.relayed_by,
    text: message.text,
    time: '',
    literal: true,
    sources: message.sources,
    changes: message.changes,
  }
}
/** Abortable backoff: transient failures keep the initial skeleton in place. */
function historyBackoff(ms: number, signal?: AbortSignal) {
  return new Promise<void>((resolve) => {
    const finish = () => {
      clearTimeout(timer)
      signal?.removeEventListener('abort', finish)
      resolve()
    }
    const timer = setTimeout(finish, ms)
    signal?.addEventListener('abort', finish, { once: true })
    if (signal?.aborted) finish()
  })
}
export async function loadHistory(
  agent: string,
  before?: string,
  signal?: AbortSignal,
  target?: string,
) {
  const stateKey = chatStateKey(agent, target)
  const previous = useChat.getState()
  const anchor = before ? undefined : previous.historyCursor[stateKey]
  const rows: ConversationMessage[] = []
  let cursor = before
  let latest: string | undefined
  let next: string | undefined
  useChat.getState().startHistory(stateKey, before)
  try {
    do {
      let messages: ConversationMessage[] = []
      for (let attempt = 0; ; attempt++) {
        if (signal?.aborted) return
        try {
          const result = await rpc.call(
            'conversation.history',
            {
              conversation_id: target ?? conversationId(agent),
              limit: pageSize,
              ...(cursor ? { before: cursor } : {}),
            },
            { signal },
          )
          messages = result.messages ?? []
          break
        } catch (error) {
          if (signal?.aborted) return
          if (unavailable(error)) useChat.getState().connection(stateKey, false)
          if (attempt === 2) throw error
          await historyBackoff(500 * 2 ** attempt, signal)
        }
      }
      if (signal?.aborted) return
      latest ??= messages.at(-1)?.id
      rows.unshift(...messages)
      next = messages.length === pageSize ? messages[0]?.id : undefined
      // History only supports a `before` cursor. Walk backwards to the last
      // successful snapshot so long outages cannot leave a gap beyond 50 rows.
      if (!anchor || messages.some((message) => message.id === anchor) || !next || next === cursor)
        break
      cursor = next
    } while (!before)
    useChat
      .getState()
      .merge(
        stateKey,
        rows.map(fromWire),
        before || !previous.historyLoaded[stateKey] ? true : 'latest',
      )
    useChat
      .getState()
      .page(
        stateKey,
        !before && previous.historyLoaded[stateKey] ? useChat.getState().older[stateKey] : next,
        undefined,
        before ? undefined : latest,
      )
    return true
  } catch (error) {
    if (signal?.aborted) return
    useChat
      .getState()
      .page(
        stateKey,
        before ?? previous.older[stateKey],
        error instanceof Error ? error.message : i18n.t('chatHistoryError'),
      )
    return false
  }
}
export function receiveEvent(agent: string, event: ConversationEvent, target?: string) {
  const stateKey = chatStateKey(agent, target)
  if (event.type === 'library.document_updated') {
    documentUpdated(event.payload as DocumentInfo)
    return
  }
  const payload = event.payload as Exclude<ConversationEvent['payload'], DocumentInfo>
  if (target && target !== conversationId(agent) && payload.conversation_id !== target) return
  if (
    event.type === 'message.created' &&
    payload.conversation_id === (target ?? conversationId(agent)) &&
    payload.message_id
  ) {
    useChat.getState().merge(stateKey, [
      fromWire({
        id: payload.message_id,
        conversation_id: payload.conversation_id,
        role: payload.role ?? 'assistant',
        text: payload.text ?? '',
        relayed_by: payload.relayed_by,
        sources: payload.sources,
        changes: payload.changes,
      }),
    ])
    if (payload.role !== 'user') {
      useChat.getState().setTyping(stateKey, false)
      useChat.getState().setActivity(stateKey)
    }
  }
  // A scoped event belongs only to that conversation. Unscoped system turns
  // use the favorite, just like the daemon's default intake path.
  const targetConversation = payload.conversation_id
  if (
    targetConversation
      ? targetConversation !== (target ?? conversationId(agent))
      : payload.agent_id
        ? payload.agent_id !== agent
        : agent !== useAgents.getState().favorite_id
  )
    return
  if (event.type === 'tool.called' || event.type === 'tool.completed') {
    if (event.type === 'tool.called') useChat.getState().setTyping(stateKey, true)
    const label =
      payload.tool === 'web_search' || payload.tool === 'source_search'
        ? 'chatSearching'
        : payload.tool === 'web_read'
          ? 'chatReading'
          : 'chatTool'
    useChat.getState().setActivity(stateKey, event.type === 'tool.called' ? label : undefined)
  }
  if (event.type === 'question.asked' || event.type === 'plan.approval_requested') {
    const id = payload.question_id ?? payload.plan_id
    if (id)
      useChat.getState().merge(stateKey, [
        {
          id: `interaction:${id}`,
          sender: 'agent',
          text: payload.content ?? payload.summary ?? '',
          literal: true,
          time: '',
          interaction: { kind: payload.question_id ? 'question' : 'plan', id },
        },
      ])
  }
  if (event.type === 'notice.posted' && payload.notice_id) {
    useChat.getState().merge(stateKey, [
      {
        id: `notice:${payload.notice_id}`,
        sender: 'agent',
        text: payload.content ?? '',
        literal: true,
        time: '',
      },
    ])
  }
  if (event.type === 'run.progress' || event.type === 'run.state_changed') {
    useChat
      .getState()
      .setTyping(
        stateKey,
        ['created', 'queued', 'running', 'verifying', 'recovering'].includes(payload.state ?? ''),
      )
  }
}
export function connectConversation(agent: string, target?: string) {
  const stateKey = chatStateKey(agent, target)
  if (!liveChatEnabled) return () => {}
  const controller = new AbortController()
  let loading = false
  let requested = false
  let socketReady = false
  let handshakes = 0
  useChat.getState().connection(stateKey, false)
  const refresh = async () => {
    if (controller.signal.aborted) return
    requested = true
    if (loading) return
    loading = true
    while (requested && !controller.signal.aborted) {
      requested = false
      if (socketReady) {
        // Only transport failures are retried automatically. Each retry keeps
        // its client_msg_id, including sends accepted before a receipt was lost.
        const queued = (useChat.getState().live[stateKey] ?? []).filter(
          (message) => message.retryOnReconnect && message.delivery === 'failed',
        )
        for (const message of queued) {
          if (controller.signal.aborted || !socketReady) break
          await sendMessage(
            agent,
            message.text,
            message.attachments,
            message,
            controller.signal,
            message.context,
            target,
          )
        }
      }
      const ok = await loadHistory(agent, undefined, controller.signal, target)
      if (!controller.signal.aborted && !requested) {
        const queued = (useChat.getState().live[stateKey] ?? []).some(
          (message) => message.retryOnReconnect && message.delivery === 'failed',
        )
        useChat.getState().connection(stateKey, ok === true && socketReady && !queued)
      }
    }
    loading = false
  }
  void refresh()
  const close = subscribeEvents({
    event: (event) => receiveEvent(agent, event, target),
    ready: () => {
      socketReady = true
      handshakes++
      if (handshakes > 1 || !loading) void refresh()
    },
    offline: () => {
      socketReady = false
      useChat.getState().connection(stateKey, false)
    },
  })
  const probe = setInterval(() => {
    if (socketReady && !useChat.getState().online[stateKey]) void refresh()
  }, 10000)
  return () => {
    controller.abort()
    clearInterval(probe)
    close()
  }
}
export async function sendMessage(
  agent: string,
  text: string,
  attachments: string[] = [],
  retry?: Message,
  signal?: AbortSignal,
  context?: ClassroomContext,
  target?: string,
) {
  const stateKey = chatStateKey(agent, target)
  const clientMsgId = retry?.clientMsgId ?? crypto.randomUUID()
  const id = retry?.id ?? `pending:${clientMsgId}`
  const appContext = retry?.context ?? context
  const message: Message = {
    ...(appContext ? { context: appContext } : {}),
    id,
    clientMsgId,
    sender: 'user',
    text,
    attachments,
    time: '',
    literal: true,
    delivery: 'sending',
    retryOnReconnect: undefined,
  }
  useChat.getState().merge(stateKey, [message])
  useChat.getState().setTyping(stateKey, true)
  try {
    const params = {
      conversation_id: target ?? conversationId(agent),
      client_msg_id: clientMsgId,
      ...(appContext ? { context: appContext } : {}),
      text,
      attachments,
    }
    // The receipt is immediate, but slow links (tunnels) need headroom.
    const receipt = await rpc.call('conversation.send', params, {
      timeoutMs: 30_000,
      ...(signal ? { signal } : {}),
    })
    useChat
      .getState()
      .patch(stateKey, id, { id: receipt.msg_id, delivery: undefined, error: undefined })
    return true
  } catch (error) {
    useChat.getState().patch(stateKey, id, {
      delivery: 'failed',
      retryOnReconnect:
        (error instanceof TransportError && error.kind !== 'aborted') || retry?.retryOnReconnect,
      error: error instanceof Error ? error.message : i18n.t('chatSendError'),
    })
    useChat.getState().setTyping(stateKey, false)
    if (unavailable(error)) useChat.getState().connection(stateKey, false)
    return false
  }
}
