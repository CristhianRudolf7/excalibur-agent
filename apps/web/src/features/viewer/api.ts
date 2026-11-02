import { useQuery } from '@tanstack/react-query'
import { rpc, RpcError, TransportError, type CallOptions } from '@/lib/rpc/client'
import type { DocumentInfo, Page, ChunkDetail } from './types'
import { seedDocument, seedPages, seedChunk } from './seeds'

// Local signatures until the library API agent lands the shared RpcMethods additions.
interface LibraryMethods {
  'library.get': { params: { id: string }; result: { document: DocumentInfo } }
  'library.pages': {
    params: { id: string; version: number; from: number; to: number }
    result: { document: DocumentInfo; pages: Page[] }
  }
  'library.chunk': {
    params: { document_id: string; chunk_id: string }
    result: { chunk: ChunkDetail }
  }
}
const call = rpc.call as <M extends keyof LibraryMethods>(
  method: M,
  params: LibraryMethods[M]['params'],
  options: CallOptions,
) => Promise<LibraryMethods[M]['result']>
export async function withSeed<T>(request: () => Promise<T>, seed: () => T, demo: boolean) {
  try {
    return { value: await request(), demo: false }
  } catch (error) {
    if (
      (error instanceof RpcError && error.code === -32601) ||
      (demo && error instanceof TransportError && error.kind !== 'aborted')
    )
      return { value: seed(), demo: true }
    throw error
  }
}
export function pageRange(page: number, count: number) {
  const bounded = Math.max(1, Math.min(Math.max(1, count), Math.floor(page)))
  const from = Math.floor((bounded - 1) / 20) * 20 + 1
  return { from, to: Math.min(Math.max(1, count), from + 19) }
}
export function useDocument(id: string) {
  return useQuery<{ value: { document: DocumentInfo }; demo: boolean }>({
    queryKey: ['library', 'get', id],
    refetchInterval: (query) =>
      ['pending', 'processing'].includes(query.state.data?.value.document.status ?? '')
        ? 1000
        : false,
    retry: false,
    staleTime: 5 * 60_000,
    queryFn: ({ signal }) =>
      withSeed(
        () => call('library.get', { id }, { signal }),
        () => ({ document: seedDocument(id) }),
        id.startsWith('demo-'),
      ),
  })
}
export function usePages(document: DocumentInfo | undefined, page: number) {
  const range = pageRange(page, document?.page_count ?? 1)
  return useQuery({
    queryKey: [
      'library',
      'pages',
      document?.id,
      document?.version,
      document?.status,
      document?.page_count,
      range,
    ],
    enabled: !!document,
    retry: false,
    staleTime: 5 * 60_000,
    queryFn: ({ signal }) =>
      withSeed(
        () =>
          call(
            'library.pages',
            { id: document!.id, version: document!.version, ...range },
            { signal },
          ),
        () => ({
          document: seedDocument(document!.id),
          pages: seedPages(document!.id).filter(
            (p) => p.number >= range.from && p.number <= range.to,
          ),
        }),
        document!.id.startsWith('demo-'),
      ),
  })
}
export function useChunk(id: string, chunkId?: string) {
  return useQuery({
    queryKey: ['library', 'chunk', id, chunkId],
    enabled: !!chunkId,
    retry: false,
    staleTime: 5 * 60_000,
    queryFn: ({ signal }) =>
      withSeed(
        () => call('library.chunk', { document_id: id, chunk_id: chunkId! }, { signal }),
        () => ({ chunk: seedChunk(id, chunkId!) }),
        id.startsWith('demo-'),
      ),
  })
}
