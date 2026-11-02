import { afterEach, expect, it, vi } from 'vitest'
import { rpc, RpcError, TransportError } from '@/lib/rpc/client'
import {
  boardOutbox,
  onBoardControl,
  openBoard,
  receiveBoardEvent,
  sendEdits,
  useWhiteboard,
} from './store'
import type { Board, Op } from './types'
const board: Board = {
  id: 'one',
  conversation_id: 'conversation',
  title: 'Ideas',
  version: 2,
  elements: [{ id: 'a', type: 'rect', x: 1, y: 2, author: 'agent' }],
  updated_at: '',
}
afterEach(() => {
  vi.restoreAllMocks()
  useWhiteboard.setState({ board: undefined, open: false, error: false })
})
it('opens the board in the requested layout and ignores controls from another conversation', async () => {
  await receiveBoardEvent('whiteboard.opened', {
    conversation_id: board.conversation_id,
    board,
    layout: 'fullscreen',
  })
  expect(useWhiteboard.getState()).toMatchObject({ open: true, layout: 'fullscreen', board })
  await receiveBoardEvent('whiteboard.closed', { conversation_id: 'other', board_id: board.id })
  expect(useWhiteboard.getState().open).toBe(true)
})
it('rebases conflicts per element with the same client operation ID and current version', async () => {
  openBoard(board)
  const newer = {
    ...board,
    version: 3,
    elements: [
      ...board.elements,
      { id: 'b', type: 'text' as const, x: 10, y: 20, text: 'Remote', author: 'agent' as const },
    ],
  }
  const call = vi
    .spyOn(rpc, 'call')
    .mockRejectedValueOnce(new RpcError(-32005, 'Stale base version', { version: 3 }))
    .mockResolvedValueOnce(newer)
    .mockResolvedValueOnce({ version: 4 })
  const ops: Op[] = [{ op: 'update', elements: [{ id: 'a', x: 99, author: 'user' }] }]
  await sendEdits(board, ops, 'stable-id')
  expect(call.mock.calls.map(([method]) => method)).toEqual([
    'whiteboard.apply',
    'whiteboard.get',
    'whiteboard.apply',
  ])
  expect(call.mock.calls[2]?.[1]).toEqual({
    board_id: 'one',
    base_version: 3,
    ops,
    client_op_id: 'stable-id',
  })
  expect(useWhiteboard.getState().board?.elements).toEqual([
    { ...board.elements[0], x: 99, author: 'user' },
    newer.elements[1],
  ])
})
it('propagates transport failures without inventing a saved version', async () => {
  openBoard(board)
  vi.spyOn(rpc, 'call').mockRejectedValue(new TransportError('Offline', 'network'))
  await expect(sendEdits(board, [], 'retry-key')).rejects.toThrow('Offline')
  expect(useWhiteboard.getState().board?.version).toBe(2)
})

it('retains queued edits and operation IDs across board remounts', () => {
  const box = boardOutbox('queue-test')
  box.pending.current.push({ op: 'remove', ids: ['a'] })
  box.batch.current = { ops: [], id: 'retry-id' }
  openBoard(board)
  expect(boardOutbox('queue-test')).toBe(box)
  expect(boardOutbox('queue-test').batch.current?.id).toBe('retry-id')
  expect(boardOutbox('queue-test').pending.current).toHaveLength(1)
})

it('does not let an interrupted animation overwrite a newly opened board', async () => {
  openBoard(board)
  let finish = () => {}
  const waiting = new Promise<void>((resolve) => {
    finish = resolve
  })
  const stop = onBoardControl(async () => waiting)
  const receive = receiveBoardEvent('whiteboard.ops', {
    conversation_id: board.conversation_id,
    board_id: board.id,
    version: 3,
    animate: true,
    ops: [{ op: 'clear' }],
  })
  await Promise.resolve()
  openBoard({ ...board, id: 'other' })
  finish()
  await receive
  expect(useWhiteboard.getState().board?.id).toBe('other')
  stop()
})

it('signals repeated open requests for the same board so the shell can restore its tab', () => {
  openBoard(board)
  const sequence = useWhiteboard.getState().openSequence
  openBoard(board)
  expect(useWhiteboard.getState().openSequence).toBe(sequence + 1)
})
