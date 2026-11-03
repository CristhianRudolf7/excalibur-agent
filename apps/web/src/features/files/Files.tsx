import { openWorkspaceFile } from './open'
import { WorkspaceExplorer } from './ProjectExplorer'
import type { FileChange } from '@/lib/rpc/contract'
import { PanelToggle } from '@/components/ui/panel-toggle'
import { loadWithSkeleton } from '@/features/work/loading'
import { PanelSkeleton } from '@/features/viewer/PanelSkeleton'
import { lazy, Suspense, useRef, useState, useEffect } from 'react'
import { useVirtualizer } from '@tanstack/react-virtual'
import { useTranslation } from 'react-i18next'
import { CheckCircle2, ChevronRight, ChevronDown, File, Folder, X } from 'lucide-react'
import { getProjectFiles } from './mock-data'
import { useFiles } from './store'
const CodeEditor = lazy(() => loadWithSkeleton(() => import('./CodeEditor')))
interface TreeRow {
  path: string
  name: string
  depth: number
  folder?: boolean
  dim?: boolean
  changed?: boolean
}
export function Files({ changes }: { changes?: FileChange[] } = {}) {
  const { t } = useTranslation()
  const state = useFiles()
  const [treeOpen, setTreeOpen] = useState(() => {
    try {
      return localStorage.getItem('files-explorer-open') !== '0'
    } catch {
      return true
    }
  })
  const toggleTree = () => {
    const next = !treeOpen
    setTreeOpen(next)
    try {
      localStorage.setItem('files-explorer-open', next ? '1' : '0')
    } catch {
      /* Optional preference. */
    }
  }

  const [cursor, setCursor] = useState([8, 3])
  return (
    <div className={`files-panel ${treeOpen ? '' : 'explorer-hidden'}`}>
      <aside className="file-explorer" inert={!treeOpen} aria-hidden={!treeOpen}>
        <h3>{t('explorer')}</h3>
        <WorkspaceExplorer demo={<DemoExplorer />} fileChanges={changes} />
      </aside>
      <div className="editor-column">
        <div className="files-tabbar">
          <div className="files-toolbar">
            <PanelToggle side="left" label="panelExplorer" open={treeOpen} onClick={toggleTree} />
          </div>
          <div role="tablist" aria-label={t('files')} className="file-tabs">
            {state.open.map((path) => (
              <div className="file-tab" key={path}>
                <button
                  role="tab"
                  aria-selected={path === state.selected}
                  onClick={() => state.select(path)}
                >
                  {t(path.split('/').at(-1)!, { defaultValue: path.split('/').at(-1)! })}
                  {state.changes[path] && <i className="changed-dot" />}
                </button>
                <button
                  aria-label={t('closeFile', { file: path.split('/').at(-1) })}
                  onClick={() => state.close(path)}
                >
                  <X />
                </button>
              </div>
            ))}
          </div>
        </div>
        <div className="breadcrumb">
          <Folder />
          {state.selected.split('/').map((part, index) => (
            <span key={`${part}-${index}`}>
              {index > 0 && <ChevronRight />}
              {t('literal', { value: part })}
            </span>
          ))}
        </div>
        <div className="code-editor">
          <Suspense fallback={<PanelSkeleton shape="editor" label={t('loadingEditor')} delayed />}>
            <CodeEditor
              key={state.selected}
              onCursor={(line, column) => setCursor([line, column])}
            />
          </Suspense>
        </div>
        <div className="editor-status">
          <span>
            <CheckCircle2 />
            {t('passed')}
          </span>
          <span>
            {t(state.selected.endsWith('.json') ? 'JSON' : 'language', { defaultValue: 'JSON' })}
          </span>
          <span>{t('encoding')}</span>
          <span>{t('cursor', { line: cursor[0], column: cursor[1] })}</span>
        </div>
      </div>
    </div>
  )
}

function DemoExplorer() {
  const { t } = useTranslation()
  const state = useFiles()
  const [folders, setFolders] = useState(new Set(['excalibur', 'src', 'src/server', 'src/tests']))
  const parent = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const parts = state.selected.split('/')
    setFolders(
      (previous) =>
        new Set([
          ...previous,
          'excalibur',
          ...parts.slice(0, -1).map((_, index) => parts.slice(0, index + 1).join('/')),
        ]),
    )
  }, [state.selected])
  const tree = new Map<string, TreeRow>()
  for (const file of [...getProjectFiles(), ...Object.values(state.changes)]) {
    const parts = file.path.split('/')
    parts.slice(0, -1).forEach((name, index) => {
      const path = parts.slice(0, index + 1).join('/')
      tree.set(path, { path, name, depth: index + 1, folder: true })
    })
    tree.set(file.path, {
      path: file.path,
      name: parts.at(-1)!,
      depth: parts.length,
      changed: !!state.changes[file.path],
    })
  }
  const all: TreeRow[] = [
    { path: 'excalibur', name: 'excalibur', depth: 0, folder: true },
    ...[...tree.values()].sort((a, b) => a.path.localeCompare(b.path)),
  ]
  const rows = all.filter(
    (row) =>
      row.path === 'excalibur' ||
      (folders.has('excalibur') &&
        row.path
          .split('/')
          .slice(0, -1)
          .every((_, index, array) => folders.has(array.slice(0, index + 1).join('/')))),
  )
  // oxlint-disable-next-line react/incompatible-library -- Virtualizer measurements intentionally remain outside React Compiler memoization.
  const virtual = useVirtualizer({
    count: rows.length,
    getScrollElement: () => parent.current,
    estimateSize: () => 32,
    overscan: 5,
  })
  return (
    <div className="file-tree" ref={parent}>
      <div style={{ height: virtual.getTotalSize(), position: 'relative' }}>
        {virtual.getVirtualItems().map((item) => {
          const row = rows[item.index]!
          const expanded = folders.has(row.path)
          return (
            <button
              key={row.path}
              className={`tree-row ${row.dim ? 'dimmed' : ''} ${state.selected === row.path ? 'selected' : ''}`}
              style={{
                position: 'absolute',
                top: item.start,
                height: item.size,
                paddingLeft: 10 + row.depth * 12,
              }}
              aria-expanded={row.folder ? expanded : undefined}
              onClick={() => {
                if (row.folder)
                  setFolders((previous) => {
                    const next = new Set(previous)
                    if (next.has(row.path)) next.delete(row.path)
                    else next.add(row.path)
                    return next
                  })
                else void openWorkspaceFile(row.path, state.changes[row.path])
              }}
            >
              {row.folder ? expanded ? <ChevronDown /> : <ChevronRight /> : <File />}
              {t('literal', { value: row.name })}
              {row.changed && <i className="changed-dot" />}
            </button>
          )
        })}
      </div>
    </div>
  )
}
