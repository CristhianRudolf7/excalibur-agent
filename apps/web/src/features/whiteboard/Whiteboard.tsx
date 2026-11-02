import { PanelSkeleton } from '@/features/viewer/PanelSkeleton'
import { loadWithSkeleton, useLoadingState } from '@/features/work/loading'
import { createPortal } from 'react-dom'
import { lazy, Suspense, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { PanelsTopLeft, PenTool } from 'lucide-react'
import { useQuery } from '@tanstack/react-query'
import { rpc } from '@/lib/rpc/client'
import { connectWhiteboards, openBoard, useWhiteboard } from './store'
import './whiteboard.css'
const Canvas = lazy(() => loadWithSkeleton(() => import('./WhiteboardCanvas')))
export function WhiteboardConnection() {
  useEffect(connectWhiteboards, [])
  return null
}
export function Whiteboard({ conversationId }: { conversationId: string }) {
  const { t } = useTranslation()
  const state = useWhiteboard()
  const [choosing, setChoosing] = useState(false)
  const boards = useQuery({
    queryKey: ['whiteboards', conversationId],
    queryFn: () => rpc.call('whiteboard.list', { conversation_id: conversationId }),
    retry: false,
  })
  const loading = useLoadingState(boards.isPending)
  const [opening, setOpening] = useState(false)
  const openingState = useLoadingState(opening)
  const active = state.open && state.board?.conversation_id === conversationId
  useEffect(() => {
    if (!active || choosing || openingState.holding || state.layout !== 'fullscreen') return
    const previous = document.activeElement as HTMLElement | null
    const host = document.querySelector<HTMLElement>('.whiteboard-fullscreen')
    host?.querySelector<HTMLElement>('button')?.focus()
    const key = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.preventDefault()
        useWhiteboard.setState({ layout: 'panel' })
      }
      if (event.key !== 'Tab' || !host) return
      const items = [
        ...host.querySelectorAll<HTMLElement>(
          'button:not([disabled]),input:not([disabled]),[tabindex="0"]',
        ),
      ].filter((item) => item.getClientRects().length > 0)
      const first = items[0],
        last = items.at(-1)
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault()
        last?.focus()
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault()
        first?.focus()
      }
    }
    document.addEventListener('keydown', key)
    return () => {
      document.removeEventListener('keydown', key)
      previous?.focus()
    }
  }, [active, choosing, openingState.holding, state.layout])
  if (active && !choosing && !openingState.holding) {
    const content = (
      <div
        className={`whiteboard-host ${state.layout === 'fullscreen' ? 'whiteboard-fullscreen' : ''}`}
        aria-modal={state.layout === 'fullscreen' ? true : undefined}
        role={state.layout === 'fullscreen' ? 'dialog' : undefined}
        aria-label={t('whiteboard')}
      >
        <button className="board-list-button" onClick={() => setChoosing(true)}>
          <PanelsTopLeft aria-hidden="true" />
          {t('boardList')}
        </button>
        <Suspense fallback={<PanelSkeleton shape="canvas" label={t('boardLoading')} delayed />}>
          <Canvas key={state.board!.id} boardId={state.board!.id} />
        </Suspense>
      </div>
    )
    return state.layout === 'fullscreen' ? createPortal(content, document.body) : content
  }
  if (openingState.holding)
    return openingState.visible ? <PanelSkeleton shape="canvas" label={t('boardLoading')} /> : null
  if (loading.holding)
    return loading.visible ? <PanelSkeleton shape="boards" label={t('boardLoading')} /> : null
  return (
    <div className="board-empty">
      <PenTool className="panel-empty-icon" aria-hidden="true" />
      <h2>{t('whiteboard')}</h2>
      <p role="status">{t(boards.isError ? 'boardOffline' : 'boardEmpty')}</p>
      <button onClick={() => void boards.refetch()}>{t('boardRetry')}</button>
      {state.error && <p role="alert">{t('boardError')}</p>}
      <div className="board-list">
        {boards.data?.boards.map((board) => (
          <button
            key={board.id}
            onClick={() => {
              setOpening(true)
              void rpc
                .call('whiteboard.get', { board_id: board.id })
                .then((value) => {
                  openBoard(value)
                  setChoosing(false)
                })
                .catch(() => state.set({ error: true }))
                .finally(() => setOpening(false))
            }}
          >
            {board.title}
          </button>
        ))}
      </div>
    </div>
  )
}
