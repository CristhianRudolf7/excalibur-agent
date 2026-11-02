/** Local mirrors of the shared library contract, Sections 2 and 5. */
export interface DocumentInfo {
  id: string
  title: string
  source_name: string
  mime: string
  format: string
  size: number
  blob_digest: string
  scope_id: string
  version: number
  status: 'pending' | 'processing' | 'ready' | 'partial' | 'failed' | 'canceled'
  progress_pages: number
  total_pages: number
  page_count: number
  paginated: boolean
  uploaded_at: string
  updated_at: string
  error: string
  failure_stage: string
  warnings: string[]
  summary: string
}
export interface SourceRef {
  n?: number
  kind: 'document' | 'web'
  title: string
  document_id?: string
  chunk_id?: string
  page?: number
  page_label?: string
  paragraph?: number
  section_path?: string
  excerpt?: string
  domain?: string
  read_at?: string
  tool?: string
  snippet?: string
  url?: string
  mime?: string
  blob_digest?: string
  verbatim?: string
  boxes?: { page: number; x0: number; y0: number; x1: number; y1: number }[]
}
export interface LibraryMethods {
  'library.ingest': {
    params: { blob_digest: string; name: string; mime: string }
    result: { document: DocumentInfo; existing: boolean }
  }
  'library.list': {
    params: { query?: string; cursor?: string; limit?: number }
    result: { documents: DocumentInfo[]; next_cursor: string }
  }
  'library.get': { params: { id: string }; result: { document: DocumentInfo } }
  'library.delete': { params: { id: string }; result: { deleted: boolean; notice: string } }
}
export type LibraryCall = <M extends keyof LibraryMethods>(
  method: M,
  params: LibraryMethods[M]['params'],
  options?: { signal?: AbortSignal },
) => Promise<LibraryMethods[M]['result']>
