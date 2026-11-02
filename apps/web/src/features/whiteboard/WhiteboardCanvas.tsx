import { PixelAvatar } from '@/components/shared'
import { useEffect, useEffectEvent, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Excalidraw, exportToBlob, exportToSvg } from '@excalidraw/excalidraw'
import type { ExcalidrawImperativeAPI } from '@excalidraw/excalidraw/types'
import { Maximize2, Minimize2, X, Download, RefreshCw } from 'lucide-react'
import { rpc } from '@/lib/rpc/client'
import { hashFile, putBlob } from '@/features/library/api'
import { fromExcalidraw, toExcalidraw } from './adapter'
import { renderScene } from './assets'
import { applyOps, diffScene, scheduleElements, revealFrames } from './scene'
import { boardOutbox, onBoardControl, sendEdits, useWhiteboard } from './store'
import type { BoardEvent, Element } from './types'
import '@excalidraw/excalidraw/index.css'

export default function WhiteboardCanvas({ boardId }: { boardId: string }) {
  const { t, i18n } = useTranslation()
  const state = useWhiteboard()
  const [api, setApi] = useState<ExcalidrawImperativeAPI | null>(null)
  const [theme, setTheme] = useState<'dark' | 'light'>(() =>
    document.documentElement.classList.contains('dark') ? 'dark' : 'light',
  )
  useEffect(() => {
    const observer = new MutationObserver(() =>
      setTheme(document.documentElement.classList.contains('dark') ? 'dark' : 'light'),
    )
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
    return () => observer.disconnect()
  }, [])
  const [manifest, setManifest] = useState('[]')
  const [saving, setSaving] = useState(false)
  const [viewport, setViewport] = useState({ x: 0, y: 0, zoom: 1 })
  const [highlight, setHighlight] = useState<BoardEvent | null>(null)
  const initialBoard = useRef(state.board)
  const scene = useRef<Element[]>([])
  const previousNative = useRef<Element[]>([])
  const applying = useRef(false)
  const [outbox] = useState(() => boardOutbox(boardId))
  const { pending, batch, sending } = outbox
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const highlightTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const animation = useRef(new AbortController())
  const rendering = useRef(0)
  const reduced = () => matchMedia('(prefers-reduced-motion: reduce)').matches
  const draw = async (elements: Element[]) => {
    if (!api) return
    const generation = ++rendering.current
    const rendered = await renderScene(elements)
    if (generation !== rendering.current || animation.current.signal.aborted) return
    applying.current = true
    api.addFiles(Object.values(rendered.files))
    previousNative.current = fromExcalidraw(
      rendered.elements.filter((element) => !element.customData?.stickyLabel),
    )
    if (!scene.current.length) {
      const viewport = useWhiteboard.getState().board?.viewport
      if (viewport)
        api.updateScene({
          appState: {
            scrollX: viewport.x,
            scrollY: viewport.y,
            zoom: { value: viewport.zoom as never },
          },
        })
    }
    api.updateScene({ elements: rendered.elements })
    scene.current = elements
    queueMicrotask(() => {
      applying.current = false
    })
  }
  const flush = async () => {
    if (sending.current || (!pending.current.length && !batch.current)) return
    const board =
      useWhiteboard.getState().board?.id === boardId
        ? useWhiteboard.getState().board
        : initialBoard.current
    if (!board) return
    sending.current = true
    setSaving(true)
    batch.current ??= { ops: pending.current.splice(0), id: crypto.randomUUID() }
    try {
      await sendEdits(board, batch.current.ops, batch.current.id)
      batch.current = null
    } catch {
      useWhiteboard.setState({ error: true })
    } finally {
      sending.current = false
      setSaving(false)
    }
    if (!batch.current && pending.current.length) void flush()
  }
  const snapshot = async (event: BoardEvent) => {
    if (!api) return
    const board = useWhiteboard.getState().board
    if (board?.id === boardId)
      await draw(applyOps(board.elements, [...(batch.current?.ops ?? []), ...pending.current]))
    const blob = await exportToBlob({
      elements: api.getSceneElements(),
      appState: api.getAppState(),
      files: api.getFiles(),
      mimeType: 'image/png',
    })
    const file = new File([blob], 'whiteboard.png', { type: 'image/png' })
    const digest = await hashFile(file)
    await putBlob(digest, file)
    await rpc.call('whiteboard.snapshot', {
      board_id: boardId,
      image_blob: digest,
      request_id: event.request_id,
    })
  }
  const resume = useEffectEvent(() => {
    void flush()
  })
  const unmount = useEffectEvent(() => {
    void flush()
    clearTimeout(timer.current)
    clearTimeout(highlightTimer.current)
    rendering.current++
  })
  useEffect(() => {
    const controller = new AbortController()
    animation.current = controller
    resume()
    return () => {
      controller.abort()
      unmount()
    }
  }, [boardId])
  useEffect(() => {
    if (!api) return
    const board = useWhiteboard.getState().board
    if (board?.id === boardId)
      void draw(
        applyOps(board.elements, [...(batch.current?.ops ?? []), ...pending.current]),
      ).catch(() => useWhiteboard.setState({ error: true }))
    // Scene updates are driven by the authoritative version, never by viewport changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [api, boardId, state.board?.version, theme])
  useEffect(() => {
    if (!api) return
    return onBoardControl(async (type, event) => {
      if (event.board_id !== boardId) return
      if (type === 'whiteboard.ops' && event.animate) {
        useWhiteboard.setState({ drawing: true })
        try {
          await scheduleElements(
            event.ops ?? [],
            event.step_ms ?? 180,
            reduced(),
            async (ops) => {
              const element = ops[0]?.elements?.[0] as Element | undefined
              if (reduced() || !element || ops[0]?.op !== 'add') {
                await draw(applyOps(scene.current, ops))
                return
              }
              for (const frame of revealFrames(element)) {
                if (animation.current.signal.aborted) return
                await draw(applyOps(scene.current, [{ op: 'add', elements: [frame] }]))
                await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()))
              }
            },
            animation.current.signal,
          )
        } finally {
          useWhiteboard.setState({ drawing: false })
        }
      }
      if (type === 'whiteboard.focus') {
        const elements = api
          .getSceneElements()
          .filter(
            (element) =>
              event.ids?.includes(element.id) ||
              event.ids?.includes(String(element.customData?.mermaidParent)),
          )
        if (event.rect)
          api.scrollToContent(
            toExcalidraw([{ ...event.rect, id: 'focus', type: 'rect', author: 'agent' }]),
            {
              fitToContent: true,
              animate: !reduced(),
              duration: 300,
              minZoom: event.zoom,
              maxZoom: event.zoom,
            },
          )
        else
          api.scrollToContent(elements.length ? elements : undefined, {
            fitToContent: true,
            animate: !reduced(),
            duration: 300,
            minZoom: event.zoom,
            maxZoom: event.zoom,
          })
      }
      if (type === 'whiteboard.highlight') {
        setHighlight(event)
        clearTimeout(highlightTimer.current)
        highlightTimer.current = setTimeout(
          () => setHighlight(null),
          Math.min(30000, event.duration_ms ?? 2200),
        )
      }
      if (type === 'whiteboard.snapshot_requested') await snapshot(event)
    })
    // Handlers read the live scene from refs and the imperative drawing API.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [api, boardId])
  const download = async (format: 'png' | 'svg' | 'json') => {
    if (!api) return
    try {
      const options = {
        elements: api.getSceneElements(),
        appState: api.getAppState(),
        files: api.getFiles(),
      }
      const blob =
        format === 'png'
          ? await exportToBlob(options)
          : format === 'svg'
            ? new Blob([(await exportToSvg(options)).outerHTML], { type: 'image/svg+xml' })
            : new Blob(
                [
                  JSON.stringify(
                    { ...useWhiteboard.getState().board, elements: scene.current },
                    null,
                    2,
                  ),
                ],
                { type: 'application/json' },
              )
      const url = URL.createObjectURL(blob),
        link = document.createElement('a')
      link.href = url
      link.download = `whiteboard.${format}`
      link.click()
      setTimeout(() => URL.revokeObjectURL(url), 1000)
    } catch {
      useWhiteboard.setState({ error: true })
    }
  }
  const bounds =
    highlight && api
      ? api
          .getSceneElements()
          .filter(
            (element) =>
              highlight.ids?.includes(element.id) ||
              highlight.ids?.includes(String(element.customData?.mermaidParent)),
          )
      : []
  const app = api?.getAppState()
  return (
    <div
      className="board-surface"
      data-testid="whiteboard-surface"
      data-elements={manifest}
      data-version={state.board?.version}
    >
      <header className="board-header">
        <div>
          <strong>{state.board?.title}</strong>
          <small>
            <i className="online-dot" />
            {t('boardPresence')}
          </small>
        </div>
        <span role="status">
          {t(state.drawing ? 'boardDrawing' : saving ? 'boardSaving' : 'boardSaved')}
        </span>
        <button
          aria-label={t(state.layout === 'fullscreen' ? 'boardPanel' : 'boardFullscreen')}
          onClick={() =>
            state.set({ layout: state.layout === 'fullscreen' ? 'panel' : 'fullscreen' })
          }
        >
          {state.layout === 'fullscreen' ? <Minimize2 /> : <Maximize2 />}
        </button>
        <button
          aria-label={t('boardClose')}
          onClick={() => {
            void flush()
            void rpc
              .call('whiteboard.close', { board_id: boardId })
              .catch(() => state.set({ error: true }))
            state.set({ open: false })
          }}
        >
          <X />
        </button>
      </header>
      {state.error && (
        <button
          className="board-error"
          onClick={() => {
            if (state.control?.type === 'whiteboard.snapshot_requested')
              void snapshot(state.control.payload)
                .then(() => state.set({ error: false, control: undefined }))
                .catch(() => state.set({ error: true }))
            else void flush()
          }}
        >
          <RefreshCw />
          {t('boardError')}
        </button>
      )}
      <div className="board-canvas" data-testid="board-canvas" data-zoom={viewport.zoom}>
        <Excalidraw
          excalidrawAPI={setApi}
          langCode={i18n.language.startsWith('es') ? 'es-ES' : 'en'}
          theme={theme}
          onChange={(elements, appState, files) => {
            setManifest(
              JSON.stringify(
                elements
                  .filter((element) => !element.isDeleted)
                  .map(({ id, type }) => ({ id, type })),
              ),
            )
            setViewport((current) =>
              current.x === appState.scrollX &&
              current.y === appState.scrollY &&
              current.zoom === appState.zoom.value
                ? current
                : { x: appState.scrollX, y: appState.scrollY, zoom: appState.zoom.value },
            )
            if (applying.current) return
            const normalized = fromExcalidraw(
              elements.filter((element) => !element.customData?.stickyLabel),
            )
            for (const element of normalized) {
              const native = elements.find((candidate) => candidate.id === element.id)
              if (
                element.type === 'image' &&
                !element.src &&
                native?.type === 'image' &&
                native.fileId &&
                files[native.fileId]
              )
                element.src = files[native.fileId]!.dataURL
            }
            const changes = diffScene(previousNative.current, normalized)
            for (const label of elements.filter(
              (element) => element.customData?.stickyLabel && 'text' in element,
            )) {
              const original = label.customData?.contract as Element
              const parent = scene.current.find((element) => element.id === original.id)
              if (parent && 'text' in label && label.text !== parent.text)
                changes.push({
                  op: 'update',
                  elements: [{ id: parent.id, text: label.text, author: 'user' }],
                })
            }
            if (!changes.length) return
            const changedIds = new Set(
              changes.flatMap((op) => [
                ...(op.ids ?? []),
                ...(op.elements ?? []).map((element) => element.id),
              ]),
            )
            const parents = new Set(
              elements
                .filter(
                  (element) => changedIds.has(element.id) && element.customData?.mermaidParent,
                )
                .map((element) => String(element.customData!.mermaidParent)),
            )
            for (const parent of parents) {
              if (
                !scene.current.some(
                  (element) => element.id === parent && element.type === 'mermaid',
                )
              )
                continue
              changes.unshift({ op: 'remove', ids: [parent] })
              changes.push({
                op: 'add',
                elements: normalized.filter(
                  (element) =>
                    elements.find((native) => native.id === element.id)?.customData
                      ?.mermaidParent === parent,
                ),
              })
            }
            previousNative.current = normalized
            for (const change of changes)
              if (change.elements)
                change.elements = change.elements.map((element) => ({ ...element, author: 'user' }))
            pending.current.push(...changes)
            scene.current = applyOps(scene.current, changes)
            clearTimeout(timer.current)
            timer.current = setTimeout(() => void flush(), 350)
          }}
        />
        {bounds.map((element) => (
          <div
            key={element.id}
            data-testid="board-highlight"
            className={`board-highlight board-${highlight?.style ?? 'laser'}`}
            style={{
              left:
                (element.x + (app?.scrollX ?? 0)) * (app?.zoom.value ?? 1) +
                (app?.offsetLeft ?? 0) -
                (api?.getAppState().offsetLeft ?? 0),
              top: (element.y + (app?.scrollY ?? 0)) * (app?.zoom.value ?? 1),
              width: Math.max(20, element.width * (app?.zoom.value ?? 1)),
              height: Math.max(20, element.height * (app?.zoom.value ?? 1)),
            }}
          />
        ))}
        {state.drawing && (
          <div className="board-cursor" aria-hidden="true">
            <PixelAvatar id="excalibur" size={22} /> Excalibur
          </div>
        )}
      </div>
      <nav className="board-exports" aria-label={t('whiteboard')}>
        {(['png', 'svg', 'json'] as const).map((format) => (
          <button
            key={format}
            aria-label={t('boardExport', { format: format.toUpperCase() })}
            onClick={() => void download(format)}
          >
            <Download size={14} />
            {format.toUpperCase()}
          </button>
        ))}
      </nav>
    </div>
  )
}
