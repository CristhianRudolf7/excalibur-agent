import { vi } from 'vitest'
export class FakeSocket {
  static peers: FakeSocket[] = []
  onopen: (() => void) | null = null
  onclose: (() => void) | null = null
  onerror: (() => void) | null = null
  onmessage: ((event: { data: string }) => void) | null = null
  send = vi.fn()
  close = vi.fn()
  readonly url: URL
  constructor(url: URL) {
    this.url = url
    FakeSocket.peers.push(this)
  }
  frame(value: unknown) {
    this.onmessage?.({ data: JSON.stringify(value) })
  }
  ack(events: unknown[] = []) {
    this.frame({ jsonrpc: '2.0', id: 'subscribe', result: { events, cursor: '' } })
  }
}
