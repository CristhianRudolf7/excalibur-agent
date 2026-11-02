import { useEffect, useRef, useState } from 'react'
import { isIngesting, libraryCall, uploadDocument, UploadError, type UploadPhase } from './api'
import { useLibraryStore } from './store'
import type { DocumentInfo } from './types'

export interface Attachment {
  key: string
  name: string
  size: number
  phase: UploadPhase | 'document' | 'error'
  percent: number
  document?: DocumentInfo
  error?: string
}
export function attachmentTransition(item: Attachment, update: Partial<Attachment>): Attachment {
  // Removed uploads are never resurrected by callers; terminal errors do not regress.
  if (item.phase === 'error') return item
  return { ...item, ...update }
}
export function useDocumentPolling(ids: string[]) {
  const key = ids.slice().sort().join('\n')
  useEffect(() => {
    if (!key) return
    const controller = new AbortController()
    let timer: ReturnType<typeof setTimeout>
    const poll = async () => {
      for (const id of key.split('\n')) {
        if (controller.signal.aborted) return
        const current = useLibraryStore.getState().documents[id]
        if (current && !isIngesting(current)) continue
        try {
          const { document } = await libraryCall(
            'library.get',
            { id },
            { signal: controller.signal },
          )
          if (!controller.signal.aborted) useLibraryStore.getState().update(document)
        } catch {
          /* Keep the last known progress; retry after a transient disconnect. */
        }
      }
      if (!controller.signal.aborted) timer = setTimeout(() => void poll(), 2000)
    }
    timer = setTimeout(() => void poll(), 2000)
    return () => {
      controller.abort()
      clearTimeout(timer)
    }
  }, [key])
}
export function useAttachments(onUploaded?: (document: DocumentInfo) => void) {
  const [items, setItems] = useState<Attachment[]>([])
  const queue = useRef(Promise.resolve())
  const controllers = useRef(new Map<string, AbortController>())
  const documents = useLibraryStore((state) => state.documents)
  useDocumentPolling(
    items.flatMap((item) =>
      item.document && isIngesting(documents[item.document.id] ?? item.document)
        ? [item.document.id]
        : [],
    ),
  )
  useEffect(() => {
    const pending = controllers.current
    return () => {
      for (const controller of pending.values()) controller.abort()
      pending.clear()
    }
  }, [])
  const add = (files: FileList | File[]) => {
    // Process one file at a time to bound simultaneous Web Crypto buffers.
    const batch = Array.from(files).map((file) => ({
      file,
      key: crypto.randomUUID(),
      controller: new AbortController(),
    }))
    for (const { key, controller } of batch) controllers.current.set(key, controller)
    setItems((previous) => [
      ...previous,
      ...batch.map(({ file, key }) => ({
        key,
        name: file.name,
        size: file.size,
        phase: 'hashing' as const,
        percent: 0,
      })),
    ])
    const patch = (key: string, update: Partial<Attachment>) =>
      setItems((previous) =>
        previous.map((item) => (item.key === key ? attachmentTransition(item, update) : item)),
      )
    queue.current = queue.current.then(async () => {
      for (const { file, key, controller } of batch) {
        if (controller.signal.aborted) continue
        try {
          const document = await uploadDocument(file, {
            signal: controller.signal,
            onProgress: (phase, percent) => patch(key, { phase, percent }),
          })
          if (!controller.signal.aborted) {
            useLibraryStore.getState().update(document)
            patch(key, { document, phase: 'document', percent: 100 })
            onUploaded?.(document)
          }
        } catch (error) {
          if (!controller.signal.aborted)
            patch(key, {
              phase: 'error',
              error:
                error instanceof UploadError
                  ? `library_${error.reason}`
                  : error instanceof Error
                    ? error.message
                    : 'library_uploadFailed',
            })
        } finally {
          controllers.current.delete(key)
        }
      }
    })
  }
  const remove = (key: string) => {
    controllers.current.get(key)?.abort()
    controllers.current.delete(key)
    setItems((previous) => previous.filter((item) => item.key !== key))
  }
  const resolved = items.map((item) => ({
    ...item,
    document: item.document ? (documents[item.document.id] ?? item.document) : undefined,
  }))
  return {
    items: resolved,
    add,
    remove,
    clear: () => {
      for (const item of items) remove(item.key)
    },
  }
}
