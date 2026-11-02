import { rpc } from '@/lib/rpc/client'
import type { LibraryCall, DocumentInfo } from './types'

// The shared contract file is owned by the library API worktree. Transport is unchanged.
export const libraryCall = rpc.call as unknown as LibraryCall
export const MAX_DOCUMENT_BYTES = 64 * 1024 * 1024
export const libraryEnabled = import.meta.env.EXCALIBUR_WEB_LIBRARY !== '0'
export class UploadError extends Error {
  readonly reason: 'tooLarge' | 'uploadFailed'
  constructor(reason: 'tooLarge' | 'uploadFailed', message: string) {
    super(message)
    this.reason = reason
  }
}
export type UploadPhase = 'hashing' | 'uploading' | 'ingesting'
export interface UploadOptions {
  signal?: AbortSignal
  onProgress?: (phase: UploadPhase, percent: number) => void
  call?: LibraryCall
  put?: typeof putBlob
}
export async function hashFile(file: File, signal?: AbortSignal): Promise<string> {
  if (file.size > MAX_DOCUMENT_BYTES)
    throw new UploadError('tooLarge', 'Documents must be 64 MiB or smaller.')
  signal?.throwIfAborted()
  // Web Crypto has no incremental digest API. Read at most one
  // 64 MiB file per upload queue; send the original File directly for upload.
  const bytes = await file.arrayBuffer()
  signal?.throwIfAborted()
  const hash = await crypto.subtle.digest('SHA-256', bytes)
  signal?.throwIfAborted()
  return Array.from(new Uint8Array(hash), (byte) => byte.toString(16).padStart(2, '0')).join('')
}
export function putBlob(
  digest: string,
  file: File,
  signal?: AbortSignal,
  progress?: (percent: number) => void,
): Promise<void> {
  return new Promise((resolve, reject) => {
    const request = new XMLHttpRequest()
    const abort = () => request.abort()
    if (signal?.aborted) {
      reject(signal.reason)
      return
    }
    request.open('PUT', `/blobs/${digest}`)
    request.setRequestHeader('Content-Type', file.type || 'application/octet-stream')
    request.upload.onprogress = (event) => {
      if (event.lengthComputable) progress?.(Math.round((event.loaded / event.total) * 100))
    }
    const finish = () => signal?.removeEventListener('abort', abort)
    request.onload = () => {
      finish()
      if (request.status >= 200 && request.status < 300) {
        progress?.(100)
        resolve()
      } else reject(new UploadError('uploadFailed', `Upload failed (HTTP ${request.status}).`))
    }
    request.onerror = () => {
      finish()
      reject(new UploadError('uploadFailed', 'Upload failed. Check your connection.'))
    }
    request.ontimeout = () => {
      finish()
      reject(new UploadError('uploadFailed', 'Upload timed out.'))
    }
    request.onabort = () => {
      finish()
      reject(new DOMException('Upload canceled', 'AbortError'))
    }
    request.timeout = 120_000
    signal?.addEventListener('abort', abort, { once: true })
    request.send(file)
  })
}
export async function uploadDocument(
  file: File,
  options: UploadOptions = {},
): Promise<DocumentInfo> {
  const { signal, onProgress } = options
  onProgress?.('hashing', 0)
  const digest = await hashFile(file, signal)
  onProgress?.('uploading', 0)
  await (options.put ?? putBlob)(digest, file, signal, (percent) =>
    onProgress?.('uploading', percent),
  )
  signal?.throwIfAborted()
  onProgress?.('ingesting', 100)
  const result = await (options.call ?? libraryCall)(
    'library.ingest',
    {
      blob_digest: digest,
      name: file.name,
      mime: file.type || 'application/octet-stream',
    },
    { signal },
  )
  return result.document
}
export function originalUrl(document: DocumentInfo) {
  return `/blobs/${encodeURIComponent(document.blob_digest)}`
}
export const isIngesting = (document: DocumentInfo) =>
  document.status === 'pending' || document.status === 'processing'
