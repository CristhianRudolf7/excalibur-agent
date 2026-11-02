import { PendingSpinner } from '@/features/work/WorkSkeleton'
import { useLoadingState } from '@/features/work/loading'
import { WorkTabs } from '@/features/work/WorkTabs'
import { validProjectPath } from './validation'
import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Folder, Plus, Search } from 'lucide-react'
import { rpc } from '@/lib/rpc/client'
import { Dialog } from '@/components/ui/dialog'
import { DataState } from '@/features/work/shared'
import { Tasks } from '@/features/tasks/Tasks'
import { ProjectExplorer } from '@/features/files/ProjectExplorer'
function ProjectDetail({ id, close }: { id: string; close: () => void }) {
  const { t } = useTranslation()
  const query = useQuery({
    queryKey: ['work', 'project', id],
    queryFn: ({ signal }) => rpc.call('project.get', { project_id: id }, { signal }),
    retry: false,
  })
  const queryLoading = useLoadingState(query.isPending)
  const [tab, setTab] = useState('tasks')
  return (
    <div className="work-project-detail">
      <button onClick={close}>{t('workBack')}</button>
      <DataState
        shape="detail"
        loading={queryLoading.visible}
        error={query.isError}
        retry={() => void query.refetch()}
      />
      {!queryLoading.holding && query.data && (
        <>
          <h2>{query.data.name}</h2>
          <p>{query.data.description}</p>
          <code>{query.data.roots.join(', ')}</code>
          <WorkTabs value={tab} label={t('projects')}>
            {['tasks', 'files'].map((value) => (
              <button
                key={value}
                role="tab"
                aria-selected={tab === value}
                onClick={() => setTab(value)}
              >
                {t(value === 'tasks' ? 'workTasks' : 'workFiles')}
              </button>
            ))}
          </WorkTabs>
          {tab === 'tasks' ? <Tasks projectId={id} /> : <ProjectExplorer projectId={id} />}
        </>
      )}
    </div>
  )
}
export function Projects({ onNotice: _onNotice }: { onNotice?: (key: string) => void }) {
  const { t, i18n } = useTranslation(),
    client = useQueryClient()
  const [text, setText] = useState(''),
    [selected, setSelected] = useState<string>(),
    [adding, setAdding] = useState(false),
    [name, setName] = useState(''),
    [path, setPath] = useState(''),
    [invalid, setInvalid] = useState(false)
  const query = useQuery({
    queryKey: ['work', 'projects'],
    queryFn: ({ signal }) => rpc.call('project.list', undefined, { signal }),
    retry: false,
  })
  const queryLoading = useLoadingState(query.isPending)
  const create = useMutation({
    mutationFn: () => rpc.call('project.register', { name: name.trim(), roots: [path.trim()] }),
    onSuccess: (project) => {
      setAdding(false)
      setSelected(project.id)
      void client.invalidateQueries({ queryKey: ['work', 'projects'] })
    },
  })
  const projects = (query.data?.projects ?? []).filter((p) =>
    `${p.name} ${p.description ?? ''} ${p.roots.join(' ')}`
      .toLocaleLowerCase()
      .includes(text.toLocaleLowerCase()),
  )
  return (
    <div className="work-page">
      <header className="work-header">
        <Folder />
        <div>
          <span className="work-eyebrow">{t('workEyebrow')}</span>
          <h1>{t('projects')}</h1>
          <p>{t('projectCount', { count: query.data?.projects?.length ?? 0 })}</p>
        </div>
        <button
          className="primary-button"
          onClick={() => {
            setAdding(true)
            setName('')
            setPath('')
            setInvalid(false)
            create.reset()
          }}
        >
          <Plus />
          {t('newProject')}
        </button>
      </header>
      {selected ? (
        <ProjectDetail key={selected} id={selected} close={() => setSelected(undefined)} />
      ) : (
        <>
          <div className="work-toolbar">
            <label className="work-search">
              <Search aria-hidden="true" />
              <input
                aria-label={t('searchProjects')}
                placeholder={t('searchProjects')}
                value={text}
                onChange={(e) => setText(e.target.value)}
              />
            </label>
          </div>
          <DataState
            shape="projects"
            loading={queryLoading.visible}
            error={query.isError}
            empty={!queryLoading.holding && !query.isError && !projects.length}
            retry={() => void query.refetch()}
          />
          <div className="work-project-grid">
            {(!queryLoading.holding ? projects : []).map((p, i) => (
              <button
                className="work-project-card"
                style={{ animationDelay: `${Math.min(i, 7) * 35}ms` }}
                key={p.id}
                onClick={() => setSelected(p.id)}
              >
                <Folder />
                <h2>{p.name}</h2>
                <p>{p.description}</p>
                <code>{p.roots.join(', ')}</code>
                <small>
                  {t('workUpdated', {
                    date: new Date(p.updated_at).toLocaleDateString(i18n.language),
                  })}
                </small>
              </button>
            ))}
          </div>
        </>
      )}
      {adding && (
        <Dialog className="work-dialog" title="newProject" onClose={() => setAdding(false)}>
          <form
            className="work-form"
            onSubmit={(e) => {
              e.preventDefault()
              if (!name.trim() || !validProjectPath(path.trim())) {
                setInvalid(true)
                return
              }
              setInvalid(false)
              create.mutate()
            }}
          >
            <label>
              {t('projectName')}
              <input autoFocus required value={name} onChange={(e) => setName(e.target.value)} />
            </label>
            <label>
              {t('projectPath')}
              <input required value={path} onChange={(e) => setPath(e.target.value)} />
            </label>
            {invalid && <p role="alert">{t('projectInvalid')}</p>}
            {create.isError && <p role="alert">{t('workMutationError')}</p>}
            <button
              className="primary-button"
              type="submit"
              aria-busy={create.isPending}
              disabled={create.isPending}
            >
              {create.isPending && <PendingSpinner />} {t('workSave')}
            </button>
          </form>
        </Dialog>
      )}
    </div>
  )
}
