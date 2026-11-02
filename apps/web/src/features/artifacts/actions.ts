import { openPanelView } from '@/features/files/open'
import { openBoard, sendEdits, useWhiteboard, whiteboardEnabled } from '@/features/whiteboard/store'
import { sanitizeSVG } from './sanitize'

export function downloadBlob(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  link.click()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}
export async function exportPNG(blob: Blob): Promise<Blob> {
  const url = URL.createObjectURL(blob)
  try {
    const image = new Image()
    await new Promise<void>((resolve, reject) => {
      image.onload = () => resolve()
      image.onerror = () => reject(new Error('Could not decode SVG'))
      image.src = url
    })
    const canvas = document.createElement('canvas')
    const scale = Math.min(1, 4096 / Math.max(image.naturalWidth, image.naturalHeight))
    canvas.width = Math.max(1, Math.round(image.naturalWidth * scale))
    canvas.height = Math.max(1, Math.round(image.naturalHeight * scale))
    const context = canvas.getContext('2d')
    if (!context) throw new Error('Canvas unavailable')
    context.drawImage(image, 0, 0, canvas.width, canvas.height)
    return await new Promise((resolve, reject) =>
      canvas.toBlob((png) => {
        if (png) resolve(png)
        else reject(new Error('Could not export PNG'))
      }, 'image/png'),
    )
  } finally {
    URL.revokeObjectURL(url)
  }
}
export async function addToWhiteboard(session: string, source: string) {
  const board = useWhiteboard.getState().board
  if (!whiteboardEnabled || !board || board.conversation_id !== session)
    throw new Error('No conversation whiteboard')
  const svg = sanitizeSVG(source)
  const root = new DOMParser().parseFromString(svg, 'image/svg+xml').documentElement
  const viewBox = root
    .getAttribute('viewBox')
    ?.trim()
    .split(/[\s,]+/)
    .map(Number)
  const dimension = (attribute: string, fallback: number) => {
    const number = Number.parseFloat(root.getAttribute(attribute) ?? '')
    return Number.isFinite(number) && number > 0 ? number : fallback
  }
  const width = viewBox?.[2] && viewBox[2] > 0 ? viewBox[2] : dimension('width', 640)
  const height = viewBox?.[3] && viewBox[3] > 0 ? viewBox[3] : dimension('height', 480)
  const scale = Math.min(1, 640 / Math.max(width, height))
  await sendEdits(
    board,
    [
      {
        op: 'add',
        elements: [
          {
            id: crypto.randomUUID(),
            type: 'image',
            x: 0,
            y:
              Math.max(0, ...board.elements.map((element) => element.y + (element.height ?? 100))) +
              32,
            width: width * scale,
            height: height * scale,
            src: `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`,
            author: 'user',
          },
        ],
      },
    ],
    crypto.randomUUID(),
  )
  const current = useWhiteboard.getState().board
  if (current?.id === board.id) {
    openBoard(current)
    openPanelView('whiteboard')
  }
}
