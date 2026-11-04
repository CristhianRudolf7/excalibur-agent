export interface FrameHeader {
  seq: number
  tab_id: string
  width: number
  height: number
  at: number
}
export interface BrowserFrame {
  header: FrameHeader
  jpeg: Uint8Array<ArrayBuffer>
}
export function parseFrame(buffer: ArrayBuffer): BrowserFrame | null {
  try {
    if (
      !(buffer instanceof ArrayBuffer) ||
      buffer.byteLength < 5 ||
      buffer.byteLength > 1_048_576 + 16_384 + 4
    )
      return null
    const size = new DataView(buffer).getUint32(0)
    if (!size || size > 16_384 || size + 4 >= buffer.byteLength) return null
    const header = JSON.parse(
      new TextDecoder('utf-8', { fatal: true }).decode(new Uint8Array(buffer, 4, size)),
    )
    if (
      !header ||
      !Number.isSafeInteger(header.seq) ||
      header.seq < 0 ||
      typeof header.tab_id !== 'string' ||
      !Number.isFinite(header.at) ||
      !Number.isFinite(header.width) ||
      !Number.isFinite(header.height) ||
      header.width <= 0 ||
      header.height <= 0 ||
      header.width > 16_384 ||
      header.height > 16_384
    )
      return null
    const jpeg = new Uint8Array(buffer, size + 4)
    if (
      jpeg.length > 1_048_576 ||
      jpeg.length < 4 ||
      jpeg[0] !== 0xff ||
      jpeg[1] !== 0xd8 ||
      jpeg[jpeg.length - 2] !== 0xff ||
      jpeg[jpeg.length - 1] !== 0xd9
    )
      return null
    return { header, jpeg }
  } catch {
    return null
  }
}
/** Owns a single watch-only viewer; no client messages are ever sent. */
export function connectBrowserFrames(
  conversation: string,
  session: string,
  frame: (value: BrowserFrame) => void,
) {
  let stopped = false
  let ended = false
  let socket: WebSocket | undefined
  let timer: ReturnType<typeof setTimeout> | undefined
  let handshake: ReturnType<typeof setTimeout> | undefined
  let failures = 0
  let delay = 1000
  const disconnect = () => {
    clearTimeout(timer)
    clearTimeout(handshake)
    if (socket) {
      socket.onclose = null
      socket.onerror = null
      socket.onmessage = null
      socket.onopen = null
      socket.close()
      socket = undefined
    }
  }
  const connect = () => {
    if (stopped || ended || document.visibilityState === 'hidden' || failures >= 4) return
    const retry = (opened: boolean) => {
      disconnect()
      if (!opened) failures++
      if (stopped || ended || failures >= 4 || document.visibilityState === 'hidden') return
      timer = setTimeout(connect, delay)
      delay = Math.min(delay * 2, 8000)
    }
    try {
      const url = new URL('/ws/browser', window.location.href)
      url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:'
      url.searchParams.set('conversation_id', conversation)
      url.searchParams.set('session_id', session)
      const peer = new WebSocket(url)
      socket = peer
      peer.binaryType = 'arraybuffer'
      let opened = false
      handshake = setTimeout(() => retry(false), 10_000)
      peer.onopen = () => {
        opened = true
        failures = 0
        clearTimeout(handshake)
      }
      peer.onmessage = ({ data }) => {
        if (socket !== peer || stopped) return
        const parsed = parseFrame(data)
        if (parsed) {
          delay = 1000
          frame(parsed)
        }
      }
      peer.onerror = () => retry(opened)
      peer.onclose = (event) => {
        if (event.code === 1000 && event.reason === 'ended') {
          ended = true
          disconnect()
        } else retry(opened)
      }
    } catch {
      retry(false)
    }
  }
  const visibility = () => {
    disconnect()
    if (document.visibilityState !== 'hidden') {
      failures = 0
      delay = 1000
      connect()
    }
  }
  document.addEventListener('visibilitychange', visibility)
  connect()
  return () => {
    stopped = true
    disconnect()
    document.removeEventListener('visibilitychange', visibility)
  }
}
