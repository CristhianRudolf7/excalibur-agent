import type { ConversationEvent, RpcMethods } from './contract'

const types = [
  'message.created',
  'question.asked',
  'plan.approval_requested',
  'tool.called',
  'tool.completed',
  'file.edited',
  'notice.posted',
  'run.progress',
  'run.state_changed',
  'library.document_updated',
  'activity.updated',
]
/** One socket with bounded reconnect delay and replay deduplication across daemon restarts. */
export function subscribeEvents(options: {
  types?: string[]
  event: (event: ConversationEvent, replayed?: boolean) => void
  ready: () => void
  offline: () => void
}) {
  let socket: WebSocket | undefined
  let timer: ReturnType<typeof setTimeout>
  let stopped = false
  let seq = 0
  const seen = new Set<string>()
  let delay = 1000
  const connect = () => {
    if (stopped) return
    // Bus sequences restart with the daemon and have no epoch. Replay the bounded
    // bus on each new socket, then deduplicate by event identity across sockets.
    seq = 0
    const retry = () => {
      if (stopped) return
      clearTimeout(timer)
      options.offline()
      if (socket) {
        socket.onclose = null
        socket.onerror = null
        socket.onmessage = null
        socket.onopen = null
        socket.close()
      }
      timer = setTimeout(connect, delay)
      delay = Math.min(delay * 2, 15000)
    }
    try {
      const url = new URL('/ws', window.location.href)
      url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:'
      const peer = new WebSocket(url)
      socket = peer
      timer = setTimeout(retry, 10000)
      peer.onopen = () => {
        peer.send(
          JSON.stringify({
            jsonrpc: '2.0',
            id: 'subscribe',
            method: 'conversation.subscribe',
            params: { cursor: '', types: options.types ?? types },
          }),
        )
      }
      const deliver = (event: ConversationEvent, replayed = false) => {
        if (!event || !event.payload) return
        // Live-only events (Robot Lab frames) carry no sequence or identity and
        // never enter replay, so they bypass the resume cursor.
        if (event.seq === 0 && !event.id) {
          options.event(event, replayed)
          return
        }
        if (!Number.isSafeInteger(event.seq) || event.seq <= seq) return
        seq = event.seq
        if (event.id && seen.has(event.id)) return
        options.event(event, replayed)
        if (event.id) seen.add(event.id)
        if (seen.size > 4096) seen.delete(seen.values().next().value!)
      }
      peer.onmessage = ({ data }) => {
        if (stopped || socket !== peer) return
        try {
          const frame = JSON.parse(String(data))
          if (frame.jsonrpc !== '2.0') return
          if (frame.id === 'subscribe') {
            if (frame.error) {
              retry()
              return
            }
            const result = frame.result as RpcMethods['conversation.subscribe']['result']
            if (!result || (result.events !== null && !Array.isArray(result.events))) {
              retry()
              return
            }
            clearTimeout(timer)
            delay = 1000
            for (const event of result.events ?? []) deliver(event, true)
            options.ready()
          } else if (frame.method === 'event') deliver(frame.params)
        } catch {
          /* Ignore malformed frames; the connection remains bounded by its handshake. */
        }
      }
      peer.onerror = () => peer.close()
      peer.onclose = retry
    } catch {
      retry()
    }
  }
  connect()
  return () => {
    stopped = true
    clearTimeout(timer)
    socket?.close()
  }
}
