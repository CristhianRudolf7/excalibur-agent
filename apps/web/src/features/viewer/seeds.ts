import type { DocumentInfo, Page, ChunkDetail } from './types'
export const seedFormats = [
  'pdf',
  'docx',
  'odt',
  'epub',
  'txt',
  'md',
  'html',
  'pptx',
  'odp',
  'xlsx',
  'ods',
] as const
export function seedDocument(id: string): DocumentInfo {
  const format = seedFormats.find((f) => id === `demo-${f}`) ?? 'txt'
  const count = ['pdf', 'pptx', 'odp', 'xlsx', 'ods'].includes(format) ? 2 : 1
  return {
    id,
    title: `Excalibur · ${format.toUpperCase()}`,
    source_name: `excalibur.${format}`,
    format,
    mime: format === 'pdf' ? 'application/pdf' : 'application/octet-stream',
    blob_digest: '',
    scope_id: 'demo',
    size: 0,
    version: 1,
    status: 'ready',
    progress_pages: count,
    total_pages: count,
    page_count: count,
    paginated: format === 'pdf',
    uploaded_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  }
}
export function seedPages(id: string): Page[] {
  const format = seedDocument(id).format
  return Array.from({ length: seedDocument(id).page_count }, (_, i) => i + 1).map((number) => ({
    number,
    title: ['xlsx', 'ods'].includes(format) ? `Sheet${number}` : `Overview ${number}`,
    width: 612,
    height: 792,
    method: 'native',
    quality: 1,
    blocks: ['xlsx', 'ods'].includes(format)
      ? [
          { kind: 'cell', anchor: `Sheet${number}!A1`, text: 'Item' },
          { kind: 'cell', anchor: `Sheet${number}!B1`, text: 'Value' },
          { kind: 'cell', anchor: `Sheet${number}!A2`, text: 'Documents' },
          { kind: 'cell', anchor: `Sheet${number}!B2`, text: '42' },
        ]
      : [
          {
            kind: 'heading',
            level: 1,
            anchor: `p${number}/title`,
            text: `Document library ${number}`,
          },
          {
            kind: ['pptx', 'odp'].includes(format) ? 'slide_text' : 'paragraph',
            anchor: `p${number}/body`,
            text: 'Exact citations keep answers connected to their sources.',
            style: ['bold'],
          },
          { kind: 'list_item', level: 1, text: 'Read, search and verify' },
          { kind: 'table', text: '| Feature | Status |\n| --- | --- |\n| Citations | Ready |' },
          { kind: 'note', text: 'Keep the original document for reference.' },
        ],
  }))
}
export function seedChunk(id: string, chunkId: string): ChunkDetail {
  return {
    document_id: id,
    chunk_id: chunkId,
    version: 1,
    page: 1,
    paragraph: 2,
    anchor: ['xlsx', 'ods'].includes(seedDocument(id).format) ? 'Sheet1!B2' : 'p1/body',
    text: 'Exact citations',
    boxes: [{ page: 1, x0: 40, y0: 90, x1: 500, y1: 120 }],
  }
}
/** Tiny generated PDF fixture; offsets are computed from ASCII bytes, never a committed binary. */
export function seedPDF(): Uint8Array {
  const stream =
    'BT /F1 20 Tf 40 720 Td (Document library) Tj 0 -35 Td /F1 12 Tf (Exact citations keep answers connected to their sources.) Tj ET'
  const objects = [
    '<< /Type /Catalog /Pages 2 0 R >>',
    '<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>',
    '<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 5 0 R >> >> /Contents 6 0 R >>',
    '<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 5 0 R >> >> /Contents 6 0 R >>',
    '<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>',
    `<< /Length ${stream.length} >>\nstream\n${stream}\nendstream`,
  ]
  let pdf = '%PDF-1.4\n'
  const offsets = [0]
  objects.forEach((object, i) => {
    offsets.push(pdf.length)
    pdf += `${i + 1} 0 obj\n${object}\nendobj\n`
  })
  const start = pdf.length
  pdf += `xref\n0 7\n0000000000 65535 f \n${offsets
    .slice(1)
    .map((offset) => `${String(offset).padStart(10, '0')} 00000 n \n`)
    .join('')}trailer\n<< /Size 7 /Root 1 0 R >>\nstartxref\n${start}\n%%EOF`
  return new TextEncoder().encode(pdf)
}
