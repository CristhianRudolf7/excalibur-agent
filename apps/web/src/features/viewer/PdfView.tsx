import { PanelSkeleton } from './PanelSkeleton'
import { useLoadingState } from '@/features/work/loading'
import { scaleBox } from './positions'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { ChunkDetail, Page } from './types'
import { blobURL } from './positions'
import { seedPDF } from './seeds'

export function PdfView({
  digest,
  page,
  chunk,
  demo,
  focusKey,
  onPageCount,
}: {
  digest: string
  page: Page
  chunk?: ChunkDetail
  demo: boolean
  focusKey?: unknown
  onPageCount?: (count: number) => void
}) {
  const { t } = useTranslation('viewer')
  const container = useRef<HTMLDivElement>(null)
  const canvas = useRef<HTMLCanvasElement>(null)
  const [available, setAvailable] = useState(600)
  const [zoom, setZoom] = useState(1)
  const [failedRender, setFailedRender] = useState<string | null>(null)
  const [attempt, setAttempt] = useState(0)
  const renderKey = `${digest}:${page.number}:${available}:${zoom}:${demo}:${attempt}`
  const [result, setResult] = useState<{
    key: string
    width: number
    height: number
    sourceWidth: number
  } | null>(null)
  const rendered = result?.key === renderKey ? result : null
  const error = failedRender === renderKey
  const loadingState = useLoadingState(!rendered && !error)
  useEffect(() => {
    const node = container.current!
    const observer = new ResizeObserver(() => setAvailable(Math.max(100, node.clientWidth - 32)))
    observer.observe(node)
    setAvailable(Math.max(100, node.clientWidth - 32))
    return () => observer.disconnect()
  }, [])
  const width = available * zoom
  useEffect(() => {
    let disposed = false
    let loading: import('pdfjs-dist').PDFDocumentLoadingTask | undefined
    let rendering: import('pdfjs-dist').RenderTask | undefined
    async function render() {
      try {
        const pdfjs = await import('pdfjs-dist')
        const worker = await import('pdfjs-dist/build/pdf.worker.min.mjs?url')
        if (disposed) return
        pdfjs.GlobalWorkerOptions.workerSrc = worker.default
        const url = blobURL(digest)
        if (!demo && !url) throw new Error('Invalid blob digest')
        loading = pdfjs.getDocument({
          ...(demo ? { data: seedPDF() } : { url: url! }),
          enableXfa: false,
        })
        const pdf = await loading.promise
        if (disposed) return
        onPageCount?.(pdf.numPages)
        const original = await pdf.getPage(page.number)
        if (disposed || !canvas.current) return
        const viewport = original.getViewport({
          scale: width / original.getViewport({ scale: 1 }).width,
        })
        // Render into an isolated canvas: canceled zoom/page effects must never
        // resize or paint the canvas owned by the next render.
        const node = document.createElement('canvas')
        const ratio = Math.min(window.devicePixelRatio || 1, 2)
        node.width = Math.floor(viewport.width * ratio)
        node.height = Math.floor(viewport.height * ratio)
        node.style.width = `${viewport.width}px`
        node.style.height = `${viewport.height}px`
        rendering = original.render({
          canvas: node,
          viewport,
          transform: [ratio, 0, 0, ratio, 0, 0],
          annotationMode: pdfjs.AnnotationMode.DISABLE,
        })
        await rendering.promise
        if (!disposed && canvas.current) {
          const visible = canvas.current
          visible.width = node.width
          visible.height = node.height
          visible.style.width = `${viewport.width}px`
          visible.style.height = `${viewport.height}px`
          visible.getContext('2d')!.drawImage(node, 0, 0)
          setResult({
            key: renderKey,
            width: viewport.width,
            height: viewport.height,
            sourceWidth: original.getViewport({ scale: 1 }).width,
          })
        }
      } catch {
        if (!disposed) setFailedRender(renderKey)
      }
    }
    void render()
    return () => {
      disposed = true
      rendering?.cancel()
      void loading?.destroy()
    }
  }, [digest, page.number, width, demo, renderKey, onPageCount])
  useEffect(() => {
    if (rendered)
      container.current?.querySelector('[data-highlight]')?.scrollIntoView?.({ block: 'center' })
  }, [rendered, chunk, focusKey])
  return (
    <div className="viewer-pdf" ref={container}>
      <div className="viewer-toolbar">
        <button onClick={() => setZoom((z) => Math.max(0.25, z - 0.25))}>{t('zoomOut')}</button>
        <button onClick={() => setZoom(1)}>{t('fit')}</button>
        <button onClick={() => setZoom((z) => Math.min(3, z + 0.25))}>{t('zoomIn')}</button>
      </div>
      {error ? (
        <div className="viewer-empty" role="alert">
          {t('pdfError')} <button onClick={() => setAttempt((a) => a + 1)}>{t('retry')}</button>
        </div>
      ) : (
        <>
          {loadingState.visible && <PanelSkeleton label={t('loading')} />}
          <div
            className="viewer-pdf-page"
            style={{
              width,
              height: rendered?.height,
              display: loadingState.holding ? 'none' : undefined,
            }}
          >
            <canvas ref={canvas} aria-label={`${t('page')} ${page.label ?? page.number}`} />
            {rendered &&
              chunk?.boxes
                ?.filter((b) => b.page === page.number)
                .map((box, i) => (
                  <div
                    key={i}
                    data-highlight="true"
                    aria-label={t('highlight')}
                    className="viewer-pdf-highlight"
                    style={scaleBox(box, rendered.width, page.width || rendered.sourceWidth)}
                  />
                ))}
          </div>
        </>
      )}
    </div>
  )
}
