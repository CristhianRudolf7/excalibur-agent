import { SceneRunControls } from './SceneRunControls'
import { lazy, Suspense, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { MessageSquare, SlidersHorizontal, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { FilterSelect } from '@/features/settings/FilterSelect'
import { PanelSkeleton } from '@/features/viewer/PanelSkeleton'
import { PanelToggle } from '@/components/ui/panel-toggle'
import { ResizeHandle } from '@/components/resize/ResizeHandle'
import { useShell } from '@/features/shell/store'
import { splitWidths, splitRatioFromPanel } from '@/features/shell/split-layout'
import type { AgentId } from '@/features/shell/mock-data'
import { useRobotEvent } from './api'
import { Chat } from '@/features/chat/Chat'
import { labCall, robotlabEnabled } from './api'
import { useLabText } from './i18n'
import { useLab, type LabSession } from './state'
import type { Experiment, FocusEvent, Pose } from './types'
import { inputThrottle } from './logic'
import { Viewer } from './Viewer'
import { useSceneRuns } from './sceneRun'
import { Controls } from './Controls'
const Experiments = lazy(() =>
  import('./Experiments').then((module) => ({ default: module.Experiments })),
)
import { VisionPanel } from './vision/VisionPanel'
import { TeleopPanel } from './teleop/TeleopPanel'
import { ToolStore } from './ToolStore'
import './robotlab.css'
export default function RobotLab({
  agentId = 'excalibur',
  mobile = false,
}: {
  agentId?: AgentId
  mobile?: boolean
}) {
  const shell = useShell()
  const split = useRef<HTMLDivElement>(null)
  const [width, setWidth] = useState(0)
  const dimensions = splitWidths(width, shell.ratio)
  const overlay = mobile || dimensions.overlay
  const { text, language } = useLabText()
  const active = useLab((state) => state.active)
  const focus = useLab((state) => state.focus)
  const sequence = useLab((state) => state.sequence)
  const [selection, setSelection] = useState<{
    sequence: number
    tab: FocusEvent['view']
    selected?: string
  }>({ sequence, tab: focus?.view ?? 'scene', selected: focus?.experiment })
  // Agent focus is external state; user choices apply until the next focus event.
  const current =
    selection.sequence === sequence
      ? selection
      : {
          sequence,
          tab: focus?.view ?? 'scene',
          selected: focus?.experiment ?? selection.selected,
        }
  const { tab, selected } = current
  const setTab = (tab: FocusEvent['view']) => setSelection({ ...current, tab })
  const setSelected = (selected: string) => setSelection({ ...current, selected })
  const [chat, setChat] = useState(false)
  const [sheet, setSheet] = useState(false)
  const sheetHost = useRef<HTMLElement>(null)
  const [weightEdits, setWeightEdits] = useState<Record<string, Record<string, number>>>({})
  const [error, setError] = useState('')
  const [notice, setNotice] = useState(false)
  const [opening, setOpening] = useState(false)
  const generation = useRef(0)
  useEffect(() => {
    if (!sheet) return
    const previous = document.activeElement
    sheetHost.current?.querySelector<HTMLButtonElement>('button')?.focus()
    return () => {
      if (previous instanceof HTMLElement && previous.isConnected) previous.focus()
    }
  }, [sheet])
  const status = useQuery({
    queryKey: ['robotlab', 'status'],
    queryFn: () => labCall('robotlab.status', {}),
    enabled: robotlabEnabled,
    retry: false,
  })
  useEffect(() => {
    const node = split.current
    if (!node) return
    const observer = new ResizeObserver(([entry]) => setWidth(entry!.contentRect.width))
    observer.observe(node)
    return () => observer.disconnect()
  }, [status.data?.installed])
  const scenarios = useQuery({
    queryKey: ['robotlab', 'scenarios'],
    queryFn: () => labCall('robotlab.scenarios', {}),
    enabled: robotlabEnabled && status.data?.installed === true,
    retry: false,
  })
  const scenario = scenarios.data?.scenarios.find((item) => item.id === active?.scene.scenario)
  const onError = useCallback((cause: unknown) => setError(String(cause)), [])
  const open = useCallback(
    async (id: string, params?: Record<string, unknown>) => {
      const epoch = ++generation.current
      setOpening(true)
      setError('')
      try {
        const result = await labCall('robotlab.session.open', { scenario: id, params })
        if (generation.current !== epoch) {
          await labCall('robotlab.session.close', { session: result.session })
          return undefined
        }
        const previous = useLab.getState().active
        useLab.getState().setActive(result)
        useSceneRuns.setState({
          session: result.session,
          playback: undefined,
          run: { mode: 'idle', previous: 'idle', received: Date.now(), pending: false },
        })
        if (previous && previous.session !== result.session)
          void labCall('robotlab.session.close', { session: previous.session }).catch(onError)
        return result
      } finally {
        if (generation.current === epoch) setOpening(false)
      }
    },
    [onError],
  )
  useEffect(() => {
    const generations = generation
    // Release the simulation when the lab unmounts or the page goes away, so
    // abandoned tabs do not keep sessions stepping on the runtime.
    const release = () => {
      const current = useLab.getState().active
      if (!current) return
      void fetch('/rpc', {
        method: 'POST',
        keepalive: true,
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          jsonrpc: '2.0',
          id: 'robotlab-release',
          method: 'robotlab.session.close',
          params: { session: current.session },
        }),
      }).catch(() => undefined)
    }
    window.addEventListener('pagehide', release)
    return () => {
      generations.current++
      window.removeEventListener('pagehide', release)
      release()
      useLab.getState().setActive(undefined)
    }
  }, [])
  const initialSession = useQuery({
    queryKey: ['robotlab', 'initial-session', scenarios.data?.scenarios[0]?.id],
    enabled: robotlabEnabled && !active && !focus?.session && !!scenarios.data?.scenarios[0],
    retry: false,
    queryFn: () => open(scenarios.data!.scenarios[0]!.id),
  })
  const weights =
    (scenario && weightEdits[scenario.id]) ??
    Object.fromEntries((scenario?.reward_terms ?? []).map((term) => [term.name, term.weight]))
  const setWeights = (next: Record<string, number>) => {
    if (scenario) setWeightEdits((previous) => ({ ...previous, [scenario.id]: next }))
  }
  useEffect(() => {
    if (!focus || !status.data?.installed) return
    let cancelled = false
    const follow = async () => {
      if (focus.session && focus.session !== useLab.getState().active?.session) {
        // The wire contract exposes a scene through reset, but no session.get.
        const { scene } = await labCall('robotlab.session.reset', { session: focus.session })
        if (!cancelled) useLab.getState().setActive({ session: focus.session, scene })
      } else if (focus.experiment && !focus.session) {
        const experiment = await labCall('robotlab.experiment.get', {
          experiment: focus.experiment,
        })
        if (!cancelled && useLab.getState().active?.scene.scenario !== experiment.scenario)
          await open(experiment.scenario, experiment.params)
      }
    }
    let timer: ReturnType<typeof setTimeout> | undefined
    void follow()
      .then(() => {
        if (cancelled) return
        setNotice(true)
        timer = setTimeout(() => setNotice(false), 6000)
      })
      .catch(onError)
    return () => {
      cancelled = true
      clearTimeout(timer)
    }
  }, [sequence, focus, status.data?.installed, open, onError])
  const session = active?.session
  const poseSender = useMemo(
    () =>
      inputThrottle((pose: Pose) => {
        if (session)
          void labCall('robotlab.session.control', { session, mode: 'pose', pose }).catch(onError)
      }),
    [session, onError],
  )
  useEffect(() => () => poseSender.cancel(), [poseSender])
  const play = async (experiment: Experiment, run: string) => {
    const result = await open(experiment.scenario, experiment.params)
    if (!result) return
    await labCall('robotlab.session.policy', {
      session: result.session,
      experiment: experiment.id,
      run,
      checkpoint: 'best',
    })
    useSceneRuns.setState({ playback: run })
    setTab('scene')
  }
  const setScene = (next: LabSession) => useLab.getState().setActive(next)
  if (!robotlabEnabled || status.data?.installed === false)
    return (
      <div className="robot-lab">
        <header className="chat-header lab-header">
          <PanelToggle
            side="left"
            label="sidebar"
            open={mobile ? shell.drawer : !shell.collapsed}
            onClick={() => shell.toggleSidebar(mobile)}
          />
          <h1>{text('title')}</h1>
        </header>
        <ToolStore />
      </div>
    )
  return (
    <div
      className={`robot-lab ${chat ? 'lab-chat-open' : ''}`}
      onKeyDown={(event) => {
        if (event.key === 'Escape') {
          if (sheet) setSheet(false)
          else setChat(false)
        }
      }}
    >
      <header className="chat-header lab-header">
        <PanelToggle
          side="left"
          label="sidebar"
          open={mobile ? shell.drawer : !shell.collapsed}
          onClick={() => shell.toggleSidebar(mobile)}
        />
        <div className="lab-heading">
          <h1>{text('title')}</h1>
        </div>
        {scenarios.data && (
          <div className="lab-scenario-switcher">
            <FilterSelect
              label={text('scenario')}
              value={scenario?.id ?? ''}
              options={scenarios.data.scenarios.map((item) => ({
                value: item.id,
                label: item.title[language],
              }))}
              onChange={(id) => {
                setTab('scene')
                void open(id).catch(onError)
              }}
            />
          </div>
        )}
        <Button
          variant="icon"
          className="icon-button lab-chat-toggle"
          aria-label={text('chat')}
          aria-expanded={chat}
          onClick={() => {
            setSheet(false)
            setChat(!chat)
          }}
        >
          <MessageSquare />
        </Button>
      </header>
      {notice && (
        <p className="lab-toast" role="status">
          {text('focus')}
        </p>
      )}
      {(error || scenarios.error || status.error || initialSession.error) && (
        <div className="lab-error" role="alert">
          {error ||
            scenarios.error?.message ||
            status.error?.message ||
            initialSession.error?.message}
          <Button
            onClick={() => {
              void scenarios.refetch()
              void status.refetch()
              if (initialSession.error) void initialSession.refetch()
            }}
          >
            {text('retry')}
          </Button>
        </div>
      )}
      <div className="lab-body" ref={split}>
        {chat && (
          <>
            <section
              className={`chat-column lab-chat ${overlay ? 'lab-chat-overlay' : ''}`}
              aria-label={text('chat')}
              style={{ width: overlay ? '100%' : dimensions.chat }}
            >
              {overlay && (
                <header className="panel-navigation">
                  <Button variant="ghost" onClick={() => setChat(false)}>
                    <X />
                    {text('backToLab')}
                  </Button>
                  {active && <SceneRunControls session={active.session} onError={onError} />}
                </header>
              )}
              <Chat key={agentId} agentId={agentId} onFile={() => {}} />
            </section>
            {!overlay && (
              <ResizeHandle
                label={text('resize')}
                value={dimensions.panel}
                min={320}
                max={Math.max(320, dimensions.available - 360)}
                defaultValue={dimensions.available / 2}
                direction={-1}
                onReset={() => shell.setRatio(0.5)}
                onHome={() => shell.setRatio(0.5)}
                onChange={(value) => {
                  const ratio = splitRatioFromPanel(value, width)
                  if (ratio !== null) shell.setRatio(ratio)
                }}
                getPointerValue={(x) => {
                  const rect = split.current?.getBoundingClientRect()
                  return rect ? rect.right - x : null
                }}
              />
            )}
          </>
        )}
        <div className="lab-workspace" inert={chat && overlay}>
          <nav
            className="lab-tabs"
            role="tablist"
            onKeyDown={(event) => {
              const buttons = [
                ...event.currentTarget.querySelectorAll<HTMLButtonElement>('[role="tab"]'),
              ]
              const index = buttons.indexOf(document.activeElement as HTMLButtonElement)
              const next =
                event.key === 'Home'
                  ? 0
                  : event.key === 'End'
                    ? buttons.length - 1
                    : event.key === 'ArrowRight'
                      ? (index + 1) % buttons.length
                      : event.key === 'ArrowLeft'
                        ? (index - 1 + buttons.length) % buttons.length
                        : -1
              if (next >= 0) {
                event.preventDefault()
                buttons[next]?.focus()
                buttons[next]?.click()
              }
            }}
          >
            {(
              [
                'scene',
                'experiment',
                ...(scenario?.has_vision ? (['vision'] as const) : []),
                ...(scenario?.inputs.includes('pose') ? (['teleop'] as const) : []),
              ] as FocusEvent['view'][]
            ).map((value) => (
              <button
                role="tab"
                key={value}
                tabIndex={tab === value ? 0 : -1}
                aria-selected={tab === value}
                onClick={() => setTab(value)}
              >
                {text(value)}
              </button>
            ))}
          </nav>
          <section className="lab-content" role="tabpanel">
            {!active || !scenario || opening ? (
              <PanelSkeleton shape="canvas" label={text('title')} />
            ) : tab === 'experiment' ? (
              <Suspense fallback={<PanelSkeleton shape="grid" label={text('experiment')} />}>
                <Experiments
                  scenario={scenario}
                  weights={weights}
                  selected={selected}
                  onSelect={setSelected}
                  onPlay={play}
                />
              </Suspense>
            ) : tab === 'vision' && scenario.has_vision ? (
              <VisionPanel session={active.session} />
            ) : tab === 'teleop' && scenario.inputs.includes('pose') ? (
              <TeleopPanel
                session={active.session}
                scene={active.scene}
                sendPose={(pose) => poseSender.push(pose)}
                viewer={<Viewer scene={active.scene} session={active.session} />}
              />
            ) : (
              <div className="lab-scene">
                <div className="lab-stage">
                  <Viewer scene={active.scene} session={active.session} />
                  <div className="lab-run-overlay">
                    <SceneRunControls session={active.session} indicatorOnly />
                  </div>
                  <Button
                    className="lab-controls-toggle"
                    aria-expanded={sheet}
                    aria-controls="lab-scene-controls"
                    onClick={() => setSheet((open) => !open)}
                  >
                    <SlidersHorizontal size={16} />
                    {text('controls')}
                  </Button>
                  <LiveHUD key={active.session} session={active.session} onError={onError} />
                </div>
                <aside
                  id="lab-scene-controls"
                  ref={sheetHost}
                  className={`lab-side ${sheet ? 'lab-side-open' : ''}`}
                  role={sheet ? 'dialog' : undefined}
                  aria-modal={sheet ? false : undefined}
                  aria-label={text('controls')}
                  onKeyDown={(event) => {
                    if (!sheet) return
                    if (event.key === 'Escape') {
                      event.preventDefault()
                      setSheet(false)
                    }
                  }}
                >
                  {sheet && (
                    <Button
                      className="lab-controls-close"
                      aria-label={text('closeControls')}
                      onClick={() => setSheet(false)}
                    >
                      <X />
                    </Button>
                  )}
                  <Controls
                    key={active.session}
                    active={active}
                    scenario={scenario}
                    onScene={setScene}
                    weights={weights}
                    onWeights={setWeights}
                    onError={onError}
                  />
                </aside>
              </div>
            )}
          </section>
        </div>
      </div>
    </div>
  )
}

/** Keep frame updates local to the HUD rather than rerendering the workspace. */
function LiveHUD({ session, onError }: { session: string; onError: (error: unknown) => void }) {
  const { text } = useLabText()
  const [hud, setHud] = useState({ step: 0, episode_return: 0 })
  useRobotEvent('robotlab.frame', (frame) => {
    if (frame.session === session && !frame.preview) setHud(frame)
  })
  return (
    <div className="lab-live-hud">
      <SceneRunControls session={session} onError={onError} />
      <span>
        {text('step')} <strong>{hud.step}</strong>
      </span>
      <span>
        {text('return')} <strong>{hud.episode_return.toFixed(1)}</strong>
      </span>
    </div>
  )
}
