import { useEffect, useRef, useState } from 'react'
import { connectBrowserFrames, type BrowserFrame, type FrameHeader } from './frameSocket'

export function useBrowserFrames(conversation: string, session?: { id: string; state: string }) {
  const canvas = useRef<HTMLCanvasElement>(null)
  const [image, setImage] = useState<{ session: string; header: FrameHeader; url?: string }>()
  const retainedURLs = useRef(new Set<string>())
  const id = session?.id
  const active = session?.state === 'active'
  useEffect(() => {
    if (!id || !active) return
    const retained = retainedURLs.current
    let stopped = false
    let busy = false
    let pending: BrowserFrame | undefined
    let latest = -1
    let objectURL: string | undefined
    const draw = async (value: BrowserFrame) => {
      busy = true
      try {
        const blob = new Blob([value.jpeg], { type: 'image/jpeg' })
        if (typeof createImageBitmap === 'function') {
          const bitmap = await createImageBitmap(blob)
          try {
            if (!stopped && canvas.current) {
              canvas.current.width = bitmap.width
              canvas.current.height = bitmap.height
              canvas.current.getContext('2d')?.drawImage(bitmap, 0, 0)
              setImage({ session: id, header: value.header })
            }
          } finally {
            bitmap.close()
          }
        } else if (!stopped) {
          const next = URL.createObjectURL(blob)
          if (objectURL) URL.revokeObjectURL(objectURL)
          objectURL = next
          setImage({ session: id, header: value.header, url: next })
        }
      } catch {
        /* A corrupt JPEG cannot interrupt the viewer. */
      } finally {
        busy = false
        const next = pending
        pending = undefined
        if (next && !stopped) void draw(next)
      }
    }
    const close = connectBrowserFrames(conversation, id, (value) => {
      if (value.header.seq <= latest) return
      latest = value.header.seq
      if (busy) pending = value
      else void draw(value)
    })
    return () => {
      stopped = true
      close()
      // Keep the last fallback image while the ended view is mounted.
      if (objectURL) retained.add(objectURL)
    }
  }, [conversation, id, active])
  useEffect(() => {
    const retained = retainedURLs.current
    return () => {
      for (const url of retained) URL.revokeObjectURL(url)
    }
  }, [])
  return { canvas, frame: image?.session === id ? image : undefined }
}
