import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { connectBrowserFrames, parseFrame } from './frameSocket'
class Socket {
  static peers: Socket[] = []
  binaryType = ''
  onopen: (() => void) | null = null
  onclose: ((event: { code: number; reason: string }) => void) | null = null
  onerror: (() => void) | null = null
  onmessage: ((event: { data: ArrayBuffer }) => void) | null = null
  close = vi.fn()
  send = vi.fn()
  url: URL
  constructor(url: URL) {
    this.url = url
    Socket.peers.push(this)
  }
}
function packet(
  header: unknown = { seq: 1, tab_id: 'tab', width: 800, height: 400, at: 1 },
  jpeg = [255, 216, 255, 217],
) {
  const bytes = new TextEncoder().encode(JSON.stringify(header))
  const buffer = new ArrayBuffer(4 + bytes.length + jpeg.length)
  new DataView(buffer).setUint32(0, bytes.length)
  new Uint8Array(buffer, 4, bytes.length).set(bytes)
  new Uint8Array(buffer, 4 + bytes.length).set(jpeg)
  return buffer
}
beforeEach(() => {
  vi.useFakeTimers()
  Socket.peers = []
  vi.stubGlobal('WebSocket', Socket)
  vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible')
})
afterEach(() => {
  vi.useRealTimers()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})
it('parses big-endian UTF-8 headers and JPEG bytes without copies', () => {
  const input = packet()
  const parsed = parseFrame(input)!
  expect(parsed.header.width).toBe(800)
  expect([...parsed.jpeg]).toEqual([255, 216, 255, 217])
  expect(parsed.jpeg.buffer).toBe(input)
})
it('rejects truncated, invalid, oversized and non-JPEG frames without throwing', () => {
  const good = { seq: 1, tab_id: 'tab', width: 800, height: 400, at: 1 }
  const malformed = [
    new ArrayBuffer(0),
    new ArrayBuffer(5),
    packet(null),
    packet({ ...good, seq: -1 }),
    packet({ ...good, width: 0 }),
    packet({ ...good, height: 20000 }),
    packet({ ...good, at: 'now' }),
    packet(good, [1, 2, 3, 4]),
    packet(good, new Array(1_048_577).fill(0)),
  ]
  const badJSON = packet()
  new Uint8Array(badJSON)[4] = 0xff
  malformed.push(badJSON)
  const badLength = packet()
  new DataView(badLength).setUint32(0, 0xffffffff)
  malformed.push(badLength)
  for (const value of malformed) expect(parseFrame(value)).toBeNull()
})
it('receives binary frames without sending and closes when disposed', () => {
  const frame = vi.fn(),
    stop = connectBrowserFrames('conversation one', 'session', frame)
  const peer = Socket.peers[0]!
  expect(peer.url.pathname).toBe('/ws/browser')
  expect(peer.url.searchParams.get('conversation_id')).toBe('conversation one')
  expect(peer.binaryType).toBe('arraybuffer')
  peer.onopen?.()
  peer.onmessage?.({ data: packet() })
  expect(frame).toHaveBeenCalledOnce()
  expect(peer.send).not.toHaveBeenCalled()
  stop()
  vi.advanceTimersByTime(30_000)
  expect(peer.close).toHaveBeenCalledOnce()
  expect(Socket.peers).toHaveLength(1)
})
it('backs off from 1 to 8 seconds on closes and never reconnects after ended', () => {
  const stop = connectBrowserFrames('c', 's', vi.fn())
  for (const delay of [1000, 2000, 4000, 8000, 8000]) {
    const count = Socket.peers.length,
      peer = Socket.peers.at(-1)!
    peer.onopen?.()
    peer.onclose?.({ code: 1012, reason: 'restart' })
    vi.advanceTimersByTime(delay - 1)
    expect(Socket.peers).toHaveLength(count)
    vi.advanceTimersByTime(1)
    expect(Socket.peers).toHaveLength(count + 1)
  }
  Socket.peers.at(-1)!.onclose?.({ code: 1000, reason: 'ended' })
  const count = Socket.peers.length
  vi.advanceTimersByTime(60_000)
  document.dispatchEvent(new Event('visibilitychange'))
  expect(Socket.peers).toHaveLength(count)
  stop()
})
it('limits failed upgrades and waits for visibility or session state to change', () => {
  const stop = connectBrowserFrames('c', 's', vi.fn())
  for (let index = 0; index < 4; index++) {
    Socket.peers.at(-1)!.onerror?.()
    vi.advanceTimersByTime(8000)
  }
  expect(Socket.peers).toHaveLength(4)
  vi.advanceTimersByTime(60_000)
  expect(Socket.peers).toHaveLength(4)
  document.dispatchEvent(new Event('visibilitychange'))
  expect(Socket.peers).toHaveLength(5)
  stop()
})
it('stops while hidden, resumes while visible, and bounds silent handshakes', () => {
  const stop = connectBrowserFrames('c', 's', vi.fn())
  vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
  document.dispatchEvent(new Event('visibilitychange'))
  expect(Socket.peers[0]!.close).toHaveBeenCalledOnce()
  vi.advanceTimersByTime(60_000)
  expect(Socket.peers).toHaveLength(1)
  vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible')
  document.dispatchEvent(new Event('visibilitychange'))
  expect(Socket.peers).toHaveLength(2)
  vi.advanceTimersByTime(11_000)
  expect(Socket.peers).toHaveLength(3)
  stop()
})
