import { WorkSkeleton } from '@/features/work/WorkSkeleton'
import { useLoadingState } from '@/features/work/loading'
import { WorkSelect } from '@/features/work/WorkSelect'
import { WorkTabs } from '@/features/work/WorkTabs'
import { TaskMetrics } from './TaskMetrics'
import { TaskStatusIcon } from './TaskStatusIcon'
import { taskStatuses, criteriaProgress } from './format'
import { useWorkEvents } from '@/features/work/events'
import { useState } from 'react'
import { useInfiniteQuery, useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { CalendarDays, Check, ClipboardCheck, LayoutGrid, List, Search } from 'lucide-react'
import { rpc } from '@/lib/rpc/client'
import type { TaskSummary, TaskStatus } from '@/lib/rpc/contract'
import { Dialog } from '@/components/ui/dialog'
import { DataState, VirtualRows } from '@/features/work/shared'

function TaskRow({ task, select }: { task: TaskSummary; select: () => void }) {
  const { t, i18n } = useTranslation()
  return (
    <button className="work-task" onClick={select}>
      <TaskStatusIcon status={task.status} />
      <span className="work-task-text">
        <small>{task.short_code}</small>
        <strong>{task.title}</strong>
        {task.blocked_reason && <small>{task.blocked_reason}</small>}
      </span>
      <span
        className="work-task-progress"
        role="img"
        aria-label={t('workProgress', { met: task.criteria_met, total: task.criteria_total })}
      >
        <span className="work-progress-track">
          <span style={{ transform: `scaleX(${criteriaProgress(task) / 100})` }} />
        </span>
        <small>
          {task.criteria_met}/{task.criteria_total}
        </small>
      </span>
      {task.due_at && (
        <time className="work-task-due" dateTime={task.due_at}>
          <CalendarDays />
          {new Date(task.due_at).toLocaleDateString(i18n.language, {
            month: 'short',
            day: 'numeric',
          })}
        </time>
      )}
      <span className={`work-badge status-${task.status}`}>{t(`work_${task.status}`)}</span>
    </button>
  )
}
function TaskDrawer({ id, close }: { id: string; close: () => void }) {
  const { t } = useTranslation()
  const detail = useQuery({
    queryKey: ['work', 'task', id],
    queryFn: ({ signal }) => rpc.call('tasks.get', { id }, { signal }),
    retry: false,
  })
  const detailLoading = useLoadingState(detail.isPending)
  const task = detail.data
  return (
    <Dialog className="work-drawer" title="workTasks" onClose={close}>
      <div className="work-detail">
        <DataState
          shape="detail"
          loading={detailLoading.visible}
          error={detail.isError}
          retry={() => void detail.refetch()}
        />
        {!detailLoading.holding && task && (
          <>
            <small>{task.short_code}</small>
            <h2>{task.title}</h2>
            <h3>{t('workRequest')}</h3>
            <p>{task.request}</p>
            <h3>{t('workRequirements')}</h3>
            <ul>
              {task.requirements.map((r) => (
                <li key={r.id}>{r.text}</li>
              ))}
            </ul>
            <h3>{t('workCriteria')}</h3>
            {task.criteria.map((c) => (
              <section key={c.id} className={`work-criterion work-criterion-${c.state}`}>
                {c.state === 'met' && <Check className="work-criterion-check" aria-hidden="true" />}
                <strong>{c.text}</strong>
                <span className="work-badge">{t(`work_${c.state}`)}</span>
                <p>{c.method}</p>
                <h4>{t('workEvidence')}</h4>
                {task.evidence
                  .filter((e) => e.criterion_id === c.id)
                  .map((e, i) => (
                    <p key={i}>
                      {t(e.ok ? 'work_met' : 'work_failed')} · {e.kind} · {e.summary}{' '}
                      <time>{e.at}</time>
                    </p>
                  ))}
              </section>
            ))}
            <h3>{t('workPlan')}</h3>
            <ol className="work-plan-timeline">
              {task.plan.map((step) => (
                <li key={step.id}>
                  <span className={`work-plan-dot work-plan-${step.status}`} aria-hidden="true" />
                  <strong>{step.title}</strong> <span>{step.status}</span>
                  {step.depends_on.length > 0 && (
                    <small>{t('workDepends', { ids: step.depends_on.join(', ') })}</small>
                  )}
                </li>
              ))}
            </ol>
            {task.notes && (
              <>
                <h3>{t('workNotes')}</h3>
                <p>{task.notes}</p>
              </>
            )}
            <h3>{t('workRuns')}</h3>
            {task.runs.map((run) => (
              <p key={run.id}>
                {run.id} · {run.state} <time>{run.started_at}</time>
                {run.ended_at && <time>{run.ended_at}</time>}
              </p>
            ))}
          </>
        )}
      </div>
    </Dialog>
  )
}
export function Tasks({
  agentId: _agentId,
  projectId,
  page = false,
}: {
  agentId?: string
  projectId?: string
  page?: boolean
}) {
  const { t } = useTranslation()
  const [text, setText] = useState(''),
    [status, setStatus] = useState<TaskStatus | ''>(''),
    [project, setProject] = useState(''),
    [board, setBoard] = useState(false),
    [selected, setSelected] = useState<string>()
  useWorkEvents()
  const projects = useQuery({
    queryKey: ['work', 'projects'],
    queryFn: ({ signal }) => rpc.call('project.list', undefined, { signal }),
    retry: false,
    enabled: !projectId,
  })
  const query = useInfiniteQuery({
    queryKey: ['work', 'tasks', text, status, projectId ?? project],
    initialPageParam: '',
    queryFn: ({ pageParam, signal }) =>
      rpc.call(
        'tasks.query',
        {
          text: text || undefined,
          status: status ? [status] : undefined,
          project_id: projectId || project || undefined,
          limit: 50,
          cursor: pageParam || undefined,
        },
        { signal },
      ),
    getNextPageParam: (last) => last.next_cursor || undefined,
    retry: false,
  })
  const queryLoading = useLoadingState(query.isPending)
  const nextLoading = useLoadingState(query.isFetchingNextPage)
  const tasks = [
    ...new Map(query.data?.pages.flatMap((p) => p.tasks).map((task) => [task.id, task])).values(),
  ]
  const more = () => {
    if (query.hasNextPage && !query.isFetchingNextPage) void query.fetchNextPage()
  }
  return (
    <div className={`work-page ${page ? '' : 'work-compact'}`}>
      <header className="work-header">
        <ClipboardCheck />
        <div>
          <span className="work-eyebrow">{t('workEyebrow')}</span>
          <h1>{t('workTasks')}</h1>
          <p>{t('workTasksSubtitle')}</p>
        </div>
        <div className={`work-view-toggle ${board ? 'is-board' : ''}`}>
          <button aria-label={t('workList')} aria-pressed={!board} onClick={() => setBoard(false)}>
            <List />
          </button>
          <button aria-label={t('workBoard')} aria-pressed={board} onClick={() => setBoard(true)}>
            <LayoutGrid />
          </button>
        </div>
      </header>
      {page &&
        (queryLoading.holding ? (
          queryLoading.visible && <WorkSkeleton shape="metrics" />
        ) : (
          <TaskMetrics counts={query.isError ? undefined : query.data?.pages[0]?.counts} />
        ))}
      <div className="work-toolbar">
        <label className="work-search">
          <Search aria-hidden="true" />
          <input
            aria-label={t('workSearch')}
            placeholder={t('workSearch')}
            value={text}
            onChange={(e) => setText(e.target.value)}
          />
        </label>
        {!projectId && (
          <WorkSelect
            label={t('workProject')}
            value={project}
            onChange={setProject}
            options={[
              { value: '', label: t('workAll') },
              ...(projects.data?.projects ?? []).map((p) => ({ value: p.id, label: p.name })),
            ]}
          />
        )}
      </div>
      <WorkTabs value={status} label={t('statusFilter')}>
        {(['', ...taskStatuses] as const).map((s) => (
          <button key={s} role="tab" aria-selected={status === s} onClick={() => setStatus(s)}>
            {t(s ? `work_${s}` : 'workAll')}{' '}
            <span>
              {s
                ? (query.data?.pages[0]?.counts[s] ?? 0)
                : Object.values(query.data?.pages[0]?.counts ?? {}).reduce((a, b) => a + b, 0)}
            </span>
          </button>
        ))}
      </WorkTabs>
      <DataState
        shape={board ? 'board' : 'tasks'}
        loading={queryLoading.visible}
        error={query.isError}
        empty={!queryLoading.holding && !query.isError && !tasks.length}
        retry={() => void query.refetch()}
      />
      {!query.isError &&
        !queryLoading.holding &&
        tasks.length > 0 &&
        (board ? (
          <div className="work-board" key="board">
            {taskStatuses
              .filter((s) => !status || s === status)
              .map((s) => (
                <section key={s}>
                  <h2>
                    <TaskStatusIcon status={s} />
                    {t(`work_${s}`)}
                    <span>{query.data?.pages[0]?.counts[s] ?? 0}</span>
                  </h2>
                  <VirtualRows
                    rows={tasks.filter((task) => task.status === s)}
                    rowKey={(task) => task.id}
                    height={150}
                    render={(task) => <TaskRow task={task} select={() => setSelected(task.id)} />}
                    more={more}
                  />
                </section>
              ))}
          </div>
        ) : (
          <div className="work-list" key="list">
            <VirtualRows
              rows={tasks}
              height={88}
              rowKey={(task) => task.id}
              render={(task) => <TaskRow task={task} select={() => setSelected(task.id)} />}
              more={more}
            />
          </div>
        ))}
      {nextLoading.visible && <WorkSkeleton rows={3} />}
      {query.hasNextPage && (
        <button
          className="work-more"
          aria-busy={query.isFetchingNextPage}
          disabled={query.isFetchingNextPage}
          onClick={more}
        >
          {t('workMore')}
        </button>
      )}
      {selected && <TaskDrawer id={selected} close={() => setSelected(undefined)} />}
    </div>
  )
}
