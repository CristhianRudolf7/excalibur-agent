import type { FileChange } from '@/lib/rpc/contract'
import { rpc } from '@/lib/rpc/client'
import { useFiles } from './store'
import { getFile } from './mock-data'
import { useViewerStore } from '../viewer/store'
import { useWorkbook } from '../spreadsheet/store'

/** Right-panel views (owned by the shell; see tasks/rp/common.md). */
export type PanelView =
  | 'home'
  | 'activity'
  | 'changes'
  | 'files'
  | 'document'
  | 'sheet'
  | 'whiteboard'
  | 'diagrams'
  | 'browser'

/** Which viewer a workspace file opens in. */
export type FileViewKind = 'code' | 'sheet' | 'document' | 'image' | 'other'

export function fileViewKind(path: string): FileViewKind {
  const ext = path.toLowerCase().split('.').pop() ?? ''
  if (['xlsx', 'xls', 'ods', 'csv', 'tsv'].includes(ext)) return 'sheet'
  if (['pdf', 'docx', 'doc', 'odt', 'pptx', 'ppt', 'odp', 'rtf'].includes(ext)) return 'document'
  if (['png', 'jpg', 'jpeg', 'gif', 'webp', 'svg'].includes(ext)) return 'image'
  if (['zip', 'tar', 'gz', 'bin', 'exe', 'so', 'dylib', 'wasm'].includes(ext)) return 'other'
  return 'code'
}

type Opener = (view: PanelView) => void
let opener: Opener | null = null

/** The shell registers how to show a right-panel view (it owns routing/state). */
export function registerPanelOpener(fn: Opener | null) {
  opener = fn
}
export function openPanelView(view: PanelView) {
  opener?.(view)
}

/** Open the real workspace content, retaining the shell-owned routing seam. */
let request = 0
export async function openWorkspaceFile(path: string, change?: FileChange): Promise<void> {
  const current = ++request
  const files = useFiles.getState()
  const known = files.changes[path]
  if (
    fileViewKind(path) === 'code' &&
    !change?.binary &&
    (change?.status === 'deleted' ||
      change?.content !== undefined ||
      known?.content !== undefined ||
      files.contents[path] !== undefined ||
      getFile(path) ||
      Object.hasOwn(files.edits, path))
  ) {
    useViewerStore.getState().setWorkspace(null)
    files.select(path, change)
    openPanelView('files')
    return
  }
  useViewerStore.getState().setWorkspace({ status: 'loading', path })
  openPanelView('document')
  try {
    const file = await rpc.call('files.open', { path })
    if (current !== request) return
    if (file.kind === 'text' && file.content !== undefined) {
      files.loadText(path, file.content)
      files.select(path, change ? { ...change, content: file.content } : undefined)
      useViewerStore.getState().setWorkspace(null)
      openPanelView('files')
    } else if (file.kind === 'sheet' && file.document_id) {
      const { loadWorkbook } = await import('../spreadsheet/loader')
      const sheets = await loadWorkbook(file.document_id)
      if (current !== request) return
      useWorkbook.getState().replaceSheets(sheets, path.split('/').pop())
      useViewerStore.getState().setWorkspace(null)
      openPanelView('sheet')
    } else if (file.kind === 'document' && file.document_id) {
      useViewerStore.getState().open({ documentId: file.document_id })
      openPanelView('document')
    } else if (
      file.kind === 'image' &&
      file.blob_url &&
      /^\/blobs\/[a-f0-9]{64}(?:\?.*)?$/i.test(file.blob_url)
    ) {
      useViewerStore.getState().setWorkspace({ status: 'image', path, url: file.blob_url })
    } else if (file.kind === 'binary') {
      useViewerStore.getState().setWorkspace({ status: 'unsupported', path })
    } else throw new Error('Missing file content')
  } catch {
    if (current !== request) return
    // Without readable content a code change can still be reviewed from its hunks.
    if (change && fileViewKind(path) === 'code' && !change.binary) {
      useViewerStore.getState().setWorkspace(null)
      files.select(path, change)
      openPanelView('files')
      return
    }
    useViewerStore.getState().setWorkspace({ status: 'error', path })
  }
}
