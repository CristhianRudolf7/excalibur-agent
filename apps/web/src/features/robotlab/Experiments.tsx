import { lazy, Suspense, useEffect, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Button } from '@/components/ui/button'
import { Segmented } from '@/features/settings/Segmented'
import { PanelSkeleton } from '@/features/viewer/PanelSkeleton'
import { RunStatus, RunToggle, type RunState } from './RunState'
import { useSceneRun, useSceneRuns } from './sceneRun'
import { useLab } from './state'
import { labCall, useRobotEvent } from './api'
import { useLabText } from './i18n'
import type { Experiment, MetricPoint, ScenarioInfo, Scene } from './types'
import { experimentState, mergeMetrics } from './logic'
import { CreateExperiment } from './CreateExperiment'
import { Viewer } from './Viewer'
import { RewardChart } from './RewardChart'
const PythonEditor = lazy(() =>
  import('@/features/files/CodeEditor').then((module) => ({ default: module.PythonEditor })),
)
export function Experiments({
  scenario,
  weights,
  selected,
  onSelect,
  onPlay,
}: {
  scenario: ScenarioInfo
  weights: Record<string, number>
  selected?: string
  onSelect: (id: string) => void
  onPlay: (experiment: Experiment, run: string) => Promise<void>
}) {
  const { text } = useLabText()
  const client = useQueryClient()
  const [creating, setCreating] = useState(false)
  const experiments = useQuery({
    queryKey: ['robotlab', 'experiments'],
    queryFn: () => labCall('robotlab.experiment.list', {}),
    retry: false,
  })
  useRobotEvent('robotlab.experiment', (experiment) => {
    client.setQueryData(['robotlab', 'experiment', experiment.id], experiment)
    void client.invalidateQueries({ queryKey: ['robotlab', 'experiments'] })
  })
  return (
    <div className="lab-experiments">
      <aside className="lab-experiment-list">
        <Button variant="primary" onClick={() => setCreating(true)}>
          {text('new')}
        </Button>
        {experiments.isPending && <PanelSkeleton shape="boards" label={text('experiment')} />}{' '}
        {experiments.error && <p role="alert">{experiments.error.message}</p>}
        {experiments.data?.experiments.map((experiment) => (
          <button
            key={experiment.id}
            aria-pressed={selected === experiment.id}
            onClick={() => onSelect(experiment.id)}
          >
            <strong>{experiment.name}</strong>
            <RunStatus
              state={experimentState(experiment.status)}
              surface="experiment"
              progress={
                experiment.status === 'running'
                  ? `${Math.max(0, ...experiment.runs.map((run) => run.steps))} / ${experiment.total_steps}`
                  : undefined
              }
            />
          </button>
        ))}
      </aside>
      <section className="lab-experiment-main">
        {selected ? (
          <ExperimentDetail key={selected} id={selected} onPlay={onPlay} />
        ) : (
          <p className="lab-empty">{text('empty')}</p>
        )}
      </section>
      {creating && (
        <CreateExperiment
          scenario={scenario}
          weights={weights}
          onClose={() => setCreating(false)}
          onCreated={(experiment) => {
            client.setQueryData(['robotlab', 'experiment', experiment.id], experiment)
            void client.invalidateQueries({ queryKey: ['robotlab', 'experiments'] })
            onSelect(experiment.id)
            setCreating(false)
          }}
        />
      )}
    </div>
  )
}
function ExperimentDetail({
  id,
  onPlay,
}: {
  id: string
  onPlay: (experiment: Experiment, run: string) => Promise<void>
}) {
  const { text } = useLabText()
  const client = useQueryClient()
  const query = useQuery({
    queryKey: ['robotlab', 'experiment', id],
    queryFn: () => labCall('robotlab.experiment.get', { experiment: id }),
    retry: false,
  })
  const experiment = query.data
  const [points, setPoints] = useState<MetricPoint[]>([])
  const [logs, setLogs] = useState<string[]>([])
  const [code, setCode] = useState<string>()
  const [error, setError] = useState('')
  const [saved, setSaved] = useState(false)
  const [busy, setBusy] = useState(false)
  const [transition, setTransition] = useState<RunState>()
  const playback = useSceneRuns((state) => state.playback)
  const activeSession = useLab((state) => state.active?.session)
  const sceneRun = useSceneRun(activeSession ?? '')
  const [tab, setTab] = useState<'overview' | 'code' | 'logs'>('overview')
  const [scene, setScene] = useState<Scene>()
  const scenario = experiment?.scenario
  const paramsKey = JSON.stringify(experiment?.params)
  useEffect(() => {
    let cancelled = false
    void labCall('robotlab.experiment.metrics', { experiment: id })
      .then((result) => {
        if (!cancelled) setPoints((previous) => mergeMetrics(result.points, previous))
      })
      .catch((cause: unknown) => {
        if (!cancelled) setError(String(cause))
      })
    return () => {
      cancelled = true
    }
  }, [id])
  useEffect(() => {
    if (!scenario || !paramsKey) return
    let cancelled = false
    void labCall('robotlab.session.open', { scenario, params: JSON.parse(paramsKey) })
      .then(async (result) => {
        if (!cancelled) setScene(result.scene)
        await labCall('robotlab.session.close', { session: result.session })
      })
      .catch((cause: unknown) => {
        if (!cancelled) setError(String(cause))
      })
    return () => {
      cancelled = true
    }
  }, [scenario, paramsKey])
  useRobotEvent('robotlab.metrics', (event) => {
    if (event.experiment === id) setPoints((previous) => mergeMetrics(previous, event.points))
  })
  useRobotEvent('robotlab.log', (event) => {
    setLogs((previous) => [...previous, `${event.source}: ${event.line}`].slice(-1000))
  })
  if (!experiment)
    return query.error ? (
      <p role="alert">{query.error.message}</p>
    ) : (
      <PanelSkeleton shape="grid" label={text('experiment')} />
    )
  const action = (kind: 'start' | 'stop') => {
    setBusy(true)
    setTransition(kind === 'start' ? 'starting' : 'stopping')
    setError('')
    void labCall(kind === 'start' ? 'robotlab.experiment.start' : 'robotlab.experiment.stop', {
      experiment: id,
    })
      .then((value) => {
        client.setQueryData(['robotlab', 'experiment', id], value)
        void client.invalidateQueries({ queryKey: ['robotlab', 'experiments'] })
      })
      .catch((cause: unknown) => setError(String(cause)))
      .finally(() => {
        setBusy(false)
        setTransition(undefined)
      })
  }
  return (
    <>
      <header className="lab-detail-heading">
        <h2>{experiment.name}</h2>
        <RunStatus
          state={transition ?? experimentState(experiment.status)}
          surface="experiment"
          progress={
            experiment.status === 'running'
              ? `${Math.max(0, ...experiment.runs.map((run) => Math.max(run.steps, ...points.filter((point) => point.run === run.id).map((point) => point.step))))} / ${experiment.total_steps}`
              : undefined
          }
        />
        <RunToggle
          active={experiment.status === 'running'}
          stop
          busy={!!transition}
          disabled={busy}
          label={text(
            transition === 'starting'
              ? 'runStarting'
              : transition === 'stopping'
                ? 'runStopping'
                : experiment.status === 'running'
                  ? 'stopTraining'
                  : 'startTraining',
          )}
          onClick={() => action(experiment.status === 'running' ? 'stop' : 'start')}
        />
      </header>
      {(error || experiment.error) && <p role="alert">{error || experiment.error}</p>}
      <Segmented
        label={text('experiment')}
        value={tab}
        options={(['overview', 'code', 'logs'] as const).map((value) => ({
          value,
          label: text(value),
        }))}
        onChange={setTab}
      />
      {tab === 'overview' ? (
        <>
          <div className="lab-previews">
            {experiment.runs.map((run) => (
              <article key={run.id}>
                <h3>
                  {run.label} · {run.algorithm}
                </h3>
                {scene ? (
                  <Viewer scene={scene} session={run.id} preview />
                ) : (
                  <PanelSkeleton shape="canvas" label={run.label} />
                )}
                <RunToggle
                  active={playback === run.id}
                  stop
                  label={text(playback === run.id ? 'playing' : 'play')}
                  title={playback === run.id ? text('stopPlayback') : text('play')}
                  disabled={!run.checkpoints.includes('best') || busy}
                  onClick={() => {
                    if (playback === run.id) {
                      void sceneRun
                        .control('idle')
                        .catch((cause: unknown) => setError(String(cause)))
                      return
                    }
                    setBusy(true)
                    void onPlay(experiment, run.id)
                      .catch((cause: unknown) => setError(String(cause)))
                      .finally(() => setBusy(false))
                  }}
                />
              </article>
            ))}
          </div>
          <RewardChart points={points} runs={experiment.runs} />
          <div className="lab-table-scroll">
            <table>
              <thead>
                <tr>
                  {['name', 'status', 'step', 'best', 'last', 'time'].map((key) => (
                    <th key={key}>{text(key as 'name')}</th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {experiment.runs.map((run) => (
                  <tr key={run.id}>
                    <td>{run.label}</td>
                    <td>
                      <RunStatus state={experimentState(run.status)} surface="experiment" />
                    </td>
                    <td>
                      {Math.max(
                        run.steps,
                        ...points
                          .filter((point) => point.run === run.id)
                          .map((point) => point.step),
                      )}
                    </td>
                    <td>
                      {(() => {
                        const returns = points
                          .filter(
                            (point) => point.run === run.id && point.episode_return !== undefined,
                          )
                          .map((point) => point.episode_return!)
                        return returns.length
                          ? Math.max(run.best_return ?? -Infinity, ...returns).toFixed(2)
                          : (run.best_return?.toFixed(2) ?? '—')
                      })()}
                    </td>
                    <td>
                      {run.last_return?.toFixed(2) ??
                        points
                          .filter(
                            (point) => point.run === run.id && point.episode_return !== undefined,
                          )
                          .at(-1)
                          ?.episode_return?.toFixed(2) ??
                        '—'}
                    </td>
                    <td>
                      {Math.max(
                        run.wall_s,
                        ...points
                          .filter((point) => point.run === run.id)
                          .map((point) => point.wall_s),
                      ).toFixed(1)}
                      s
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </>
      ) : tab === 'code' ? (
        <>
          <div className="lab-code">
            <Suspense fallback={<PanelSkeleton shape="editor" label={text('code')} />}>
              <PythonEditor
                value={code ?? experiment.code}
                onChange={(value) => {
                  setCode(value)
                  setSaved(false)
                }}
              />
            </Suspense>
          </div>
          <Button
            disabled={busy}
            onClick={() => {
              setBusy(true)
              setError('')
              void labCall('robotlab.experiment.code', {
                experiment: id,
                code: code ?? experiment.code,
              })
                .then((result) => {
                  if (!result.ok) setError(result.errors?.join('\n') ?? text('failed'))
                  else {
                    setSaved(true)
                    client.setQueryData(['robotlab', 'experiment', id], {
                      ...experiment,
                      code: code ?? experiment.code,
                    })
                  }
                })
                .catch((cause: unknown) => setError(String(cause)))
                .finally(() => setBusy(false))
            }}
          >
            {text('save')}
          </Button>
          {saved && <p role="status">{text('saved')}</p>}
        </>
      ) : (
        <pre className="lab-logs" role="log">
          {logs.join('\n')}
        </pre>
      )}
    </>
  )
}
