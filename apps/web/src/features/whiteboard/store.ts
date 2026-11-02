import { create } from 'zustand'
import { rpc } from '@/lib/rpc/client'
import { subscribeEvents } from '@/lib/rpc/events'
import { applyOps, VersionBuffer } from './scene'
import type { Board, BoardEvent } from './types'
interface State {
  board?: Board
  layout: 'panel' | 'fullscreen'
  open: boolean
  openSequence: number
  drawing: boolean
  error: boolean
  control?: { type: string; payload: BoardEvent; nonce: number }
  set: (state: Partial<State>) => void
}
export const useWhiteboard = create<State>((set) => ({
  layout: 'panel',
  open: false,
  openSequence: 0,
  drawing: false,
  error: false,
  set,
}))
export const whiteboardEnabled = import.meta.env.EXCALIBUR_WEB_WHITEBOARD !== '0'
let buffer: VersionBuffer | undefined
let boardKey: string | undefined
let epoch = 0
const controllers = new Set<(type: string, event: BoardEvent) => Promise<void>>()
export function onBoardControl(callback: (type: string, event: BoardEvent) => Promise<void>) {
  controllers.add(callback)
  const control = useWhiteboard.getState().control
  if (control?.type === 'whiteboard.snapshot_requested') {
    void callback(control.type, control.payload)
      .then(() => {
        if (useWhiteboard.getState().control === control)
          useWhiteboard.setState({ control: undefined })
      })
      .catch(() => useWhiteboard.setState({ error: true }))
  }
  return () => {
    controllers.delete(callback)
  }
}
export function openBoard(board: Board, layout: State['layout'] = 'panel') {
  epoch++
  buffer = undefined
  boardKey = undefined
  useWhiteboard.setState({
    openSequence: useWhiteboard.getState().openSequence + 1,
    board,
    layout,
    open: true,
    error: false,
    control: undefined,
    drawing: false,
  })
}
export async function receiveBoardEvent(type: string, event: BoardEvent) {
  if (!whiteboardEnabled || !type.startsWith('whiteboard.')) return
  const state = useWhiteboard.getState()
  if (type === 'whiteboard.opened' && event.board) {
    openBoard(event.board, event.layout)
    return
  }
  if (
    !state.board ||
    event.board_id !== state.board.id ||
    event.conversation_id !== state.board.conversation_id
  )
    return
  if (type === 'whiteboard.closed') {
    state.set({ open: false })
    return
  }
  if (type === 'whiteboard.ops') {
    if (!buffer || boardKey !== event.board_id) {
      boardKey = event.board_id
      const generation = epoch
      let latest = state.board
      buffer = new VersionBuffer(
        () => (generation === epoch ? useWhiteboard.getState().board! : latest),
        (board) => {
          latest = board
          if (generation === epoch) useWhiteboard.setState({ board })
        },
        () => rpc.call('whiteboard.get', { board_id: event.board_id! }),
        async (payload) => {
          if (generation !== epoch) return
          await Promise.all([...controllers].map((callback) => callback('whiteboard.ops', payload)))
        },
      )
    }
    await buffer.receive(event)
  } else {
    state.set({ control: { type, payload: event, nonce: Date.now() } })
    await Promise.all([...controllers].map((callback) => callback(type, event)))
    if (
      type === 'whiteboard.snapshot_requested' &&
      controllers.size &&
      useWhiteboard.getState().control?.payload === event
    )
      state.set({ control: undefined })
  }
}
export function connectWhiteboards() {
  if (!whiteboardEnabled) return () => {}
  return subscribeEvents({
    types: [
      'whiteboard.opened',
      'whiteboard.ops',
      'whiteboard.focus',
      'whiteboard.highlight',
      'whiteboard.snapshot_requested',
      'whiteboard.closed',
    ],
    event: (event) => {
      void receiveBoardEvent(event.type, event.payload as BoardEvent).catch(() =>
        useWhiteboard.setState({ error: true }),
      )
    },
    ready: () => {
      const state = useWhiteboard.getState()
      if (state.open && state.board)
        void rpc
          .call('whiteboard.get', { board_id: state.board.id })
          .then((board) => {
            if (useWhiteboard.getState().board?.id === board.id) state.set({ board, error: false })
          })
          .catch(() => state.set({ error: true }))
    },
    offline: () => {
      if (useWhiteboard.getState().open) useWhiteboard.setState({ error: true })
    },
  })
}
/** Failed batches retain their idempotency key; conflicts rebase per element. */
export async function sendEdits(board: Board, ops: import('./types').Op[], clientOpId: string) {
  let base = board
  for (let attempt = 0; attempt < 3; attempt++) {
    try {
      const { version } = await rpc.call('whiteboard.apply', {
        board_id: base.id,
        base_version: base.version,
        ops,
        client_op_id: clientOpId,
      })
      const current = useWhiteboard.getState().board
      if (current?.id === base.id)
        useWhiteboard.setState({
          board: {
            ...current,
            elements: applyOps(current.elements, ops),
            version: Math.max(current.version, version),
          },
          error: false,
        })
      return
    } catch (error) {
      const { RpcError } = await import('@/lib/rpc/client')
      if (
        !(error instanceof RpcError) ||
        !(
          error.code === -32005 ||
          /conflict/i.test(error.message) ||
          (error.data && typeof error.data === 'object' && 'current_version' in error.data)
        )
      )
        throw error
      base = await rpc.call('whiteboard.get', { board_id: base.id })
      if (useWhiteboard.getState().board?.id === base.id) useWhiteboard.setState({ board: base })
    }
  }
  throw new Error('Conflict retry limit')
}

interface Outbox {
  pending: { current: import('./types').Op[] }
  batch: { current: { ops: import('./types').Op[]; id: string } | null }
  sending: { current: boolean }
}
const outboxes = new Map<string, Outbox>()
/** Keep unsent batches across panel/fullscreen remounts and board navigation. */
export function boardOutbox(id: string): Outbox {
  let box = outboxes.get(id)
  if (!box) {
    box = { pending: { current: [] }, batch: { current: null }, sending: { current: false } }
    outboxes.set(id, box)
  }
  return box
}
