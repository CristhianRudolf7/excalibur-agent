import { afterEach, expect, it, vi } from 'vitest'
import { webcrypto } from 'node:crypto'
import { File as NodeFile } from 'node:buffer'
import { hashFile, MAX_DOCUMENT_BYTES, putBlob, uploadDocument } from './api'
import { seedDocuments } from './seed'
import type { LibraryCall } from './types'

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})
function file() {
  return new NodeFile(['abc'], 'notes.txt', { type: 'text/plain' }) as unknown as File
}
it('hashes exact file bytes with SHA-256 and checks the limit before reading', async () => {
  vi.stubGlobal('crypto', webcrypto)
  expect(await hashFile(file())).toBe(
    'ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad',
  )
  const arrayBuffer = vi.fn()
  await expect(
    hashFile({ size: MAX_DOCUMENT_BYTES + 1, arrayBuffer } as unknown as File),
  ).rejects.toMatchObject({ reason: 'tooLarge' })
  expect(arrayBuffer).not.toHaveBeenCalled()
})
it('uploads the blob before submitting metadata to the existing RPC transport', async () => {
  vi.stubGlobal('crypto', webcrypto)
  const sequence: string[] = []
  const fetch = vi.fn(async (_url: string, options: RequestInit) => {
    const request = JSON.parse(options.body as string)
    sequence.push(request.method)
    expect(request.params).toEqual({
      blob_digest: 'ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad',
      name: 'notes.txt',
      mime: 'text/plain',
    })
    return new Response(
      JSON.stringify({
        jsonrpc: '2.0',
        id: request.id,
        result: { document: seedDocuments[0], existing: false },
      }),
    )
  })
  vi.stubGlobal('fetch', fetch)
  const put = vi.fn(
    async (
      digest: string,
      original: File,
      _signal?: AbortSignal,
      progress?: (value: number) => void,
    ) => {
      sequence.push('PUT')
      expect(digest).toHaveLength(64)
      expect(original.name).toBe('notes.txt')
      progress?.(45)
    },
  )
  const progress = vi.fn()
  expect(await uploadDocument(file(), { put, onProgress: progress })).toEqual(seedDocuments[0])
  expect(sequence).toEqual(['PUT', 'library.ingest'])
  expect(progress.mock.calls).toEqual([
    ['hashing', 0],
    ['uploading', 0],
    ['uploading', 45],
    ['ingesting', 100],
  ])
})
it('does not ingest after a failed or canceled upload', async () => {
  vi.stubGlobal('crypto', webcrypto)
  const call = vi.fn() as unknown as LibraryCall
  await expect(
    uploadDocument(file(), {
      put: async () => {
        throw new Error('HTTP 500')
      },
      call,
    }),
  ).rejects.toThrow('HTTP 500')
  expect(call).not.toHaveBeenCalled()
  const controller = new AbortController()
  controller.abort()
  const put = vi.fn()
  await expect(uploadDocument(file(), { signal: controller.signal, put, call })).rejects.toThrow()
  expect(put).not.toHaveBeenCalled()
})
it('uses PUT at the digest URL and reports actual byte progress', async () => {
  const request = {
    open: vi.fn(),
    setRequestHeader: vi.fn(),
    upload: {} as { onprogress: (event: object) => void },
    send: vi.fn(),
    status: 201,
    onload: () => {},
    abort: vi.fn(),
  }
  vi.stubGlobal(
    'XMLHttpRequest',
    class {
      constructor() {
        return request
      }
    },
  )
  const progress = vi.fn()
  const original = file()
  const pending = putBlob('abc', original, undefined, progress)
  expect(request.open).toHaveBeenCalledWith('PUT', '/blobs/abc')
  expect(request.send).toHaveBeenCalledWith(original)
  request.upload.onprogress({ lengthComputable: true, loaded: 1, total: 2 })
  request.onload()
  await pending
  expect(progress.mock.calls).toEqual([[50], [100]])
})
it('rejects non-success HTTP results from blob upload', async () => {
  const request = {
    open: vi.fn(),
    setRequestHeader: vi.fn(),
    upload: {},
    send: vi.fn(),
    status: 413,
    onload: () => {},
  }
  vi.stubGlobal(
    'XMLHttpRequest',
    class {
      constructor() {
        return request
      }
    },
  )
  const pending = putBlob('abc', file())
  request.onload()
  await expect(pending).rejects.toThrow('HTTP 413')
})
it('aborts an active HTTP upload without submitting ingestion', async () => {
  const request = {
    open: vi.fn(),
    setRequestHeader: vi.fn(),
    upload: {},
    send: vi.fn(),
    onabort: () => {},
    abort: vi.fn(() => request.onabort()),
  }
  vi.stubGlobal(
    'XMLHttpRequest',
    class {
      constructor() {
        return request
      }
    },
  )
  const controller = new AbortController()
  const pending = putBlob('abc', file(), controller.signal)
  controller.abort()
  await expect(pending).rejects.toMatchObject({ name: 'AbortError' })
  expect(request.abort).toHaveBeenCalledOnce()
})
