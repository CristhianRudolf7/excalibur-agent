import { cleanup, render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, expect, it, vi } from 'vitest'
import '@/lib/i18n'
import { rpc } from '@/lib/rpc/client'
import { Whiteboard } from './Whiteboard'
import { useWhiteboard } from './store'
vi.mock('./WhiteboardCanvas', () => ({
  default: ({ boardId }: { boardId: string }) => <div>Canvas for {boardId}</div>,
}))
vi.mock('@/features/work/loading', () => ({
  loadWithSkeleton: (load: () => Promise<unknown>) => load(),
  useLoadingState: (busy: boolean) => ({ holding: busy, visible: busy }),
}))
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  useWhiteboard.setState({ board: undefined, open: false, layout: 'panel' })
})
it('embeds exactly the requested board without listing unrelated boards', async () => {
  const call = vi.spyOn(rpc, 'call').mockResolvedValue({
    id: 'lesson-board',
    title: 'Lesson',
    conversation_id: 'shared-conversation',
    version: 0,
    elements: [],
    updated_at: '',
  } as never)
  render(
    <QueryClientProvider client={new QueryClient()}>
      <Whiteboard conversationId="cnv_web_excalibur" boardId="lesson-board" />
    </QueryClientProvider>,
  )
  expect(
    await screen.findByText('Canvas for lesson-board', {}, { timeout: 10_000 }),
  ).toBeInTheDocument()
  expect(call).toHaveBeenCalledWith('whiteboard.get', { board_id: 'lesson-board' })
  expect(call.mock.calls.some(([method]) => method === 'whiteboard.list')).toBe(false)
  expect(useWhiteboard.getState().board?.id).toBe('lesson-board')
})
