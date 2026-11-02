import { useQueries, useQuery } from '@tanstack/react-query'
import { useEffect, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { ChevronDown, ChevronRight, File } from 'lucide-react'
import { rpc } from '@/lib/rpc/client'
import type { FileChange, Project } from '@/lib/rpc/contract'
import { Select } from '@/components/ui/select'
import { DataState, VirtualRows } from '@/features/work/shared'
import { openWorkspaceFile } from './open'
import { useFiles } from './store'
import './i18n'
import '@/styles/redesign/explorer.css'

import { treeEntries, workspacePath } from './tree'

export function WorkspaceExplorer({
  demo,
  fileChanges,
}: {
  demo: ReactNode
  fileChanges?: FileChange[]
}) {
  const { t } = useTranslation()
  const [selected, setSelected] = useState(() => {
    try {
      return localStorage.getItem('files-project') ?? ''
    } catch {
      return ''
    }
  })
  const query = useQuery({
    queryKey: ['work', 'projects'],
    queryFn: ({ signal }) => rpc.call('project.list', undefined, { signal }),
    retry: false,
  })
  const projects = query.data?.projects ?? []
  const active = projects.find((project) => project.id === selected) ?? projects[0]
  if (!active) {
    if (query.isPending) return <DataState shape="files" loading retry={() => {}} />
    return demo
  }
  return (
    <div className="workspace-explorer">
      {projects.length > 1 && (
        <Select
          label={t('explorerProject')}
          value={active.id}
          options={projects.map((project) => ({ value: project.id, label: project.name }))}
          onValueChange={(id) => {
            setSelected(id)
            try {
              localStorage.setItem('files-project', id)
            } catch {
              /* Optional preference. */
            }
          }}
        />
      )}
      <ProjectExplorer
        key={active.id}
        projectId={active.id}
        project={active}
        fileChanges={fileChanges}
      />
    </div>
  )
}

/** Flat virtual rows preserve the explorer look while directories load on expansion. */
export function ProjectExplorer({
  projectId,
  project,
  fileChanges,
}: {
  projectId: string
  project?: Project
  fileChanges?: FileChange[]
}) {
  const { t } = useTranslation()
  const selected = useFiles((state) => state.selected)
  const storedChanges = useFiles((state) => state.changes)
  const changes = fileChanges
    ? Object.fromEntries(fileChanges.map((change) => [change.path, change]))
    : storedChanges
  const [folders, setFolders] = useState(new Set(['']))
  const detail = useQuery({
    queryKey: ['work', 'project', projectId],
    queryFn: ({ signal }) => rpc.call('project.get', { project_id: projectId }, { signal }),
    enabled: !project,
    retry: false,
  })
  const current = project ?? detail.data
  const root = current?.roots[0]
  useEffect(() => {
    if (!root) return
    const prefix = `${root.replace(/\/$/, '')}/`
    if (!selected.startsWith(prefix)) return
    const parts = selected.slice(prefix.length).split('/').slice(0, -1)
    // oxlint-disable-next-line react/set-state-in-effect -- Reveal the file selected by the external Files store.
    setFolders(
      (previous) =>
        new Set([...previous, '', ...parts.map((_, index) => parts.slice(0, index + 1).join('/'))]),
    )
  }, [root, selected])
  const paths = [...folders].filter(
    (path) =>
      folders.has('') &&
      path
        .split('/')
        .slice(0, -1)
        .every((_, index, parts) => folders.has(parts.slice(0, index + 1).join('/'))),
  )
  const queries = useQueries({
    queries: paths.map((path) => ({
      queryKey: ['work', 'files', projectId, path],
      queryFn: async ({ signal }: { signal: AbortSignal }) => {
        const entries: string[] = []
        let cursor: string | undefined
        do {
          const page = await rpc.call(
            'files.list',
            { project_id: projectId, path, cursor, limit: 100 },
            { signal },
          )
          entries.push(...(page.entries ?? []))
          cursor = page.next_cursor || undefined
        } while (cursor)
        return entries
      },
      retry: false,
      enabled: !!current,
    })),
  })
  const rows: { path: string; folder: boolean; dim?: boolean; depth: number }[] = []
  function append(path: string, depth: number) {
    const query = queries[paths.indexOf(path)]
    for (const entry of treeEntries(query?.data ?? [])) {
      rows.push({ ...entry, depth })
      if (entry.folder && folders.has(entry.path)) append(entry.path, depth + 1)
    }
  }
  if (folders.has('')) append('', 1)
  const toggle = (path: string) =>
    setFolders((previous) => {
      const next = new Set(previous)
      if (next.has(path)) next.delete(path)
      else next.add(path)
      return next
    })
  return (
    <section className="project-explorer" aria-label={t('explorer')}>
      <button className="tree-row" aria-expanded={folders.has('')} onClick={() => toggle('')}>
        {folders.has('') ? <ChevronDown /> : <ChevronRight />}
        {current?.name ?? t('workFiles')}
      </button>
      <DataState
        shape="files"
        loading={!current && detail.isPending}
        error={detail.isError && !project}
        retry={() => void detail.refetch()}
      />
      {queries.map((query, index) => (
        <DataState
          key={paths[index]}
          shape="files"
          loading={query.isPending && query.fetchStatus !== 'idle'}
          error={query.isError}
          empty={!!query.data && !query.data.length}
          retry={() => void query.refetch()}
        />
      ))}
      <VirtualRows
        rows={rows}
        rowKey={(row) => row.path}
        height={32}
        render={(row) => {
          const absolute = workspacePath(current?.roots[0] ?? '', row.path)
          const changed = changes[absolute] ?? changes[row.path]
          const expanded = folders.has(row.path)
          return (
            <button
              className={`tree-row ${row.dim ? 'dimmed' : ''} ${selected === absolute ? 'selected' : ''}`}
              style={{ paddingLeft: 10 + row.depth * 12 }}
              aria-expanded={row.folder ? expanded : undefined}
              title={row.path}
              onClick={() => {
                if (row.folder) toggle(row.path)
                else if (current?.roots[0]) void openWorkspaceFile(absolute, changed)
              }}
            >
              {row.folder ? expanded ? <ChevronDown /> : <ChevronRight /> : <File />}
              <span>{row.path.split('/').at(-1)}</span>
              {changed && <i className="changed-dot" />}
            </button>
          )
        }}
      />
    </section>
  )
}
