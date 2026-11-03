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
export async function loadHistory(agent: string, before?: string, signal?: AbortSignal) {
  try {
    const { messages } = await rpc.call(
      'conversation.history',
      { conversation_id: conversationId(agent), limit: pageSize, ...(before ? { before } : {}) },
      { signal },
    )
    if (signal?.aborted) return
    const rows = messages ?? []
    useChat.getState().merge(agent, rows.map(fromWire), true)
    useChat.getState().page(agent, rows.length === pageSize ? rows[0]?.id : undefined)
    return true
  } catch (error) {
    if (signal?.aborted) return
    useChat
      .getState()
      .page(agent, before, error instanceof Error ? error.message : i18n.t('chatHistoryError'))
    if (unavailable(error)) useChat.getState().connection(agent, false)
    return false
  }
}
export function receiveEvent(agent: string, event: ConversationEvent) {
  if (event.type === 'library.document_updated') {
    documentUpdated(event.payload as DocumentInfo)
    return
  }
  const payload = event.payload as Exclude<ConversationEvent['payload'], DocumentInfo>
  if (
    event.type === 'message.created' &&
    payload.conversation_id === conversationId(agent) &&
    payload.message_id
  ) {
    useChat.getState().merge(agent, [
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
      useChat.getState().setTyping(agent, false)
      useChat.getState().setActivity(agent)
    }
  }
  // A scoped event belongs only to that conversation. Unscoped system turns
  // use the favorite, just like the daemon's default intake path.
  const targetConversation = payload.conversation_id
  if (
    targetConversation
      ? targetConversation !== conversationId(agent)
      : payload.agent_id
        ? payload.agent_id !== agent
        : agent !== useAgents.getState().favorite_id
  )
    return
  if (event.type === 'tool.called' || event.type === 'tool.completed') {
    if (event.type === 'tool.called') useChat.getState().setTyping(agent, true)
    const label =
      payload.tool === 'web_search' || payload.tool === 'source_search'
        ? 'chatSearching'
        : payload.tool === 'web_read'
          ? 'chatReading'
          : 'chatTool'
    useChat.getState().setActivity(agent, event.type === 'tool.called' ? label : undefined)
  }
  if (event.type === 'question.asked' || event.type === 'plan.approval_requested') {
    const id = payload.question_id ?? payload.plan_id
    if (id)
      useChat.getState().merge(agent, [
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
    useChat.getState().merge(agent, [
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
        agent,
        ['created', 'queued', 'running', 'verifying', 'recovering'].includes(payload.state ?? ''),
      )
  }
}
export function connectConversation(agent: string) {
  if (!liveChatEnabled) return () => {}
  const controller = new AbortController()
  let loading = false
  let socketReady = false
  useChat.getState().connection(agent, false)
  const refresh = async () => {
    if (loading || controller.signal.aborted) return
    loading = true
    const ok = await loadHistory(agent, undefined, controller.signal)
    if (!controller.signal.aborted) useChat.getState().connection(agent, ok === true && socketReady)
    loading = false
  }
  void refresh()
  const close = subscribeEvents({
    event: (event) => receiveEvent(agent, event),
    ready: () => {
      socketReady = true
      void refresh()
    },
    offline: () => {
      socketReady = false
      useChat.getState().connection(agent, false)
    },
  })
  const probe = setInterval(() => {
    if (socketReady && !useChat.getState().online[agent]) void refresh()
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
) {
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
  }
  useChat.getState().merge(agent, [message])
  useChat.getState().setTyping(agent, true)
  try {
    const params = {
      conversation_id: conversationId(agent),
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
      .patch(agent, id, { id: receipt.msg_id, delivery: undefined, error: undefined })
    return true
  } catch (error) {
    useChat.getState().patch(agent, id, {
      delivery: 'failed',
      error: error instanceof Error ? error.message : i18n.t('chatSendError'),
    })
    useChat.getState().setTyping(agent, false)
    if (unavailable(error)) useChat.getState().connection(agent, false)
    return false
  }
}
