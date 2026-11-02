/** Local mirrors of the document library wire contract, Section 2. */
export interface DocumentInfo {
  id: string
  title: string
  source_name: string
  mime: string
  format: string
  blob_digest: string
  scope_id: string
  size: number
  version: number
  status: string
  progress_pages: number
  total_pages: number
  page_count: number
  paginated: boolean
  uploaded_at: string
  updated_at: string
  error?: string
  failure_stage?: string
  warnings?: string[]
  summary?: string
}
export interface Box {
  page: number
  x0: number
  y0: number
  x1: number
  y1: number
}
export interface Block {
  kind: string
  level?: number
  text: string
  anchor?: string
  boxes?: Box[]
  style?: string[]
  image?: string
}
export interface Page {
  number: number
  label?: string
  title?: string
  width?: number
  height?: number
  method: string
  quality: number
  blocks: Block[]
  warnings?: string[]
}
export interface ChunkDetail {
  chunk_id: string
  document_id: string
  version: number
  page: number
  page_label?: string
  paragraph: number
  section_path?: string
  anchor: string
  text: string
  boxes: Box[]
}
