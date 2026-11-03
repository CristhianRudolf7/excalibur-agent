import { useCallback, useEffect, useRef, useState } from 'react'
import { useStore } from 'zustand'
import { useNavigate } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import {
  ArrowLeft,
  BookOpen,
  Mic,
  Send,
  X,
  Settings,
  CircleCheck,
  CircleHelp,
  Coffee,
  Map as MapIcon,
} from 'lucide-react'
import { useFiles } from '@/features/files/store'
import { connectConversation, sendMessage } from '@/features/chat/live'
import { useTasks } from '@/features/tasks/store'
import type { Panel } from '@/features/shell/panels'
import { manualWorldClock } from './debug'
import { WorldCanvas } from './WorldCanvas'
import { worldSession } from './session'
import type { WorldStore } from './store'
import type { Deliverable, Point } from './layout'
import { behaviorFor, destinationFor } from './activity-map'
import { worldSpeed, type EventProducer } from './simulation'
import { createMockProducer } from './seed/producer'
import { connectChat } from './chat-bridge'
import { MissionBoard } from './Missions'
import { ActivityFeed } from './Activity'
import { Minimap } from './Minimap'
import { MapModal } from './MapModal'
import { hitPolygon } from './layout'
import { WorldSettings } from './WorldSettings'
import { cameraViewport } from './minimap-transform'
import { DialogQueue } from './Dialog'
import type { WorldEngine } from './engine/WorldEngine'
import './world.css'
function preference(key: string, fallback: boolean) {
  try {
    const stored = localStorage.getItem(key)
    return stored === null ? fallback : stored === '1'
  } catch {
    return fallback
  }
}
/** Application composition: supplies a session to reusable views and bridges frontend chat and the mock event producer. */
export default function World({
  roomId,
  agentId,
  onRoom,
  onOpen,
  store = worldSession,
  producerFactory = createMockProducer,
}: {
  roomId?: string
  agentId: string
  onRoom: (id?: string) => void
  onOpen: (agent: string, panel: Panel) => void
  store?: WorldStore
  producerFactory?: (elapsed: number, epoch: number) => EventProducer
}) {
  const { t, i18n } = useTranslation()
  const navigate = useNavigate()
  const speed = useStore(store, (state) => state.speed),
    zoom = useStore(store, (state) => state.zoom)
  const layout = useStore(store, (state) => state.layout),
    agents = useStore(store, (state) => state.agents),
    identities = useStore(store, (state) => state.identities)
  const queue = useStore(store, (state) => state.queue),
    activity = useStore(store, (state) => state.activity),
    missions = useStore(store, (state) => state.missions),
    camera = useStore(store, (state) => state.camera)
  const room = layout.rooms.find((entry) => entry.id === roomId)
  const [text, setText] = useState(''),
    [preview, setPreview] = useState<Deliverable | null>(null),
    [microphone, setMicrophone] = useState(false)
  const mobile = window.matchMedia('(max-width: 767px)').matches
  const [missionsCollapsed, setMissionsCollapsed] = useState(mobile),
    [activityCollapsed, setActivityCollapsed] = useState(
      () => mobile || preference('world-activity', false),
    ),
    [mapOpen, setMapOpen] = useState(false),
    [mapPositions, setMapPositions] = useState<Record<string, Point>>({}),
    [unread, setUnread] = useState(0)
  const [size, setSize] = useState({ width: 1200, height: 800 })
  const input = useRef<HTMLInputElement>(null),
    root = useRef<HTMLDivElement>(null),
    labels = useRef<HTMLDivElement>(null),
    engine = useRef<WorldEngine | null>(null),
    bottom = useRef<HTMLDivElement>(null),
    pendingMapFocus = useRef<Point | null>(null),
    simulationAdvance = useRef<((milliseconds: number) => void) | null>(null)
  const advanceSimulation = useCallback(
    (milliseconds: number) => simulationAdvance.current?.(milliseconds),
    [],
  )
  const selected = room?.mainAgentId ?? agentId,
    selectedAgent = identities[selected]
  const sharedTasks = useTasks((state) => state.tasks)
  const openRoom = useCallback((id: string) => onRoom(id), [onRoom])
  useEffect(() => {
    const observer = new ResizeObserver(([entry]) => {
      if (entry) setSize({ width: entry.contentRect.width, height: entry.contentRect.height })
    })
    observer.observe(root.current!)
    const overlays = new ResizeObserver(([entry]) => {
      if (entry)
        root.current?.style.setProperty('--world-bottom-height', `${entry.contentRect.height}px`)
    })
    if (bottom.current) overlays.observe(bottom.current)
    return () => {
      observer.disconnect()
      overlays.disconnect()
    }
  }, [])
  useEffect(() => {
    const media = window.matchMedia('(max-width: 767px)')
    const compact = () => {
      if (media.matches) {
        setMissionsCollapsed(true)
        setActivityCollapsed(true)
      }
    }
    media.addEventListener('change', compact)
    return () => media.removeEventListener('change', compact)
  }, [])
  useEffect(() => connectChat(store, () => i18n.language), [store, i18n])
  useEffect(() => {
    const producer = producerFactory(
      store.getState().elapsed,
      Date.now() - store.getState().elapsed,
    )
    const search = new URLSearchParams(window.location.search)
    if (
      search.has('worldSpeed') &&
      (import.meta.env.MODE !== 'production' || search.get('worldDebug') === '1')
    )
      store.getState().setSpeed(worldSpeed(window.location.search, true))
    const advance = (milliseconds: number) => {
      for (const event of producer.advance(milliseconds * store.getState().speed))
        store.getState().emit(event)
      store.getState().setElapsed(producer.elapsed)
    }
    simulationAdvance.current = advance
    let last = performance.now()
    const timer = manualWorldClock()
      ? undefined
      : setInterval(() => {
          const now = performance.now(),
            bounds = root.current?.getBoundingClientRect()
          if (!document.hidden && bounds && bounds.bottom > 0 && bounds.top < innerHeight)
            advance(now - last)
          last = now
        }, 250)
    return () => {
      clearInterval(timer)
      if (simulationAdvance.current === advance) simulationAdvance.current = null
    }
  }, [store, producerFactory])
  useEffect(() => {
    let previous = store.getState().activity
    return store.subscribe((state) => {
      if (state.activity !== previous && activityCollapsed) setUnread((count) => count + 1)
      previous = state.activity
    })
  }, [store, activityCollapsed])
  useEffect(() => {
    const id = queue[0]?.target.roomId
    if (id) engine.current?.focusRoom(id)
  }, [queue])
  useEffect(() => {
    const key = (event: KeyboardEvent) => {
      if (event.defaultPrevented || mapOpen) return
      if (event.key === 'Escape' && room) {
        event.preventDefault()
        onRoom(undefined)
      }
      if (
        event.key === 'Enter' &&
        !store.getState().queue.length &&
        event.target instanceof HTMLElement &&
        !event.target.matches('input, textarea, button, select') &&
        !event.target.isContentEditable
      ) {
        event.preventDefault()
        input.current?.focus()
      }
    }
    window.addEventListener('keydown', key)
    return () => window.removeEventListener('keydown', key)
  }, [room, onRoom, store, mapOpen])
  useEffect(() => connectConversation(agentId), [agentId])
  const send = () => {
    if (!text.trim() || !selectedAgent) return
    void sendMessage(agentId, text.trim())
    setText('')
  }
  const toggleActivity = () => {
    if (mobile) setMissionsCollapsed(true)
    const collapsed = !activityCollapsed
    setActivityCollapsed(collapsed)
    setUnread(0)
    try {
      localStorage.setItem('world-activity', collapsed ? '1' : '0')
    } catch {
      /* Preferences are optional. */
    }
  }
  const visibleMissions = missions
    .filter((mission) => !room || mission.roomId === room.id)
    .slice(0, room ? 4 : 3)
    .map((mission) => {
      const task = sharedTasks.find((task) => task.id === mission.id)
      return task
        ? {
            ...mission,
            status:
              task.status === 'done'
                ? ('done' as const)
                : task.status === 'blocked'
                  ? ('blocked' as const)
                  : mission.status,
            progress: task.status === 'done' ? 100 : mission.progress,
          }
        : mission
    })
  const latest = new Map(
    activity
      .map((event) => event.agentId)
      .map((id) => [id, activity.find((event) => event.agentId === id)!]),
  )
  const markers = Object.values(agents).map((agent) => ({
    id: agent.id,
    position:
      (mapOpen ? mapPositions[agent.id] : undefined) ??
      (latest.get(agent.id)
        ? (destinationFor(latest.get(agent.id)!, layout)?.spot ?? layout.plaza)
        : (layout.rooms.find((room) => room.id === agent.homeRoomId)?.spots.desk ?? layout.plaza)),
    color: agent.sprite.tone,
    active: agent.id === (queue[0]?.agentId ?? selected),
  }))
  const openMap = () => {
    const positions: Record<string, Point> = {}
    for (const [id, actor] of engine.current?.agents ?? []) positions[id] = { ...actor.position }
    setMapPositions(positions)
    setMapOpen(true)
  }
  const mapShapes = layout.rooms.map((entry) => ({
    id: entry.id,
    points: entry.polygon,
    label: entry.label,
    color: identities[entry.mainAgentId ?? '']?.sprite.tone,
    agents: Object.values(agents)
      .filter((agent) => {
        const marker = markers.find((marker) => marker.id === agent.id)
        return marker && hitPolygon(marker.position, entry.polygon)
      })
      .map((agent) => ({ id: agent.id, name: agent.name, sprite: agent.sprite })),
    missionCount: missions.filter(
      (mission) => mission.roomId === entry.id && mission.status !== 'done',
    ).length,
  }))
  const onEngineReady = useCallback((ready: WorldEngine) => {
    if (pendingMapFocus.current) {
      ready.focusPoint(pendingMapFocus.current, true)
      pendingMapFocus.current = null
    }
  }, [])
  const icons = {
    working: Settings,
    done: CircleCheck,
    blocked: CircleHelp,
    question: CircleHelp,
    idle: Coffee,
  }
  return (
    <div className={`world ${room ? 'world-interior' : ''}`} ref={root} data-testid="world">
      <WorldCanvas
        store={store}
        roomId={room?.id}
        onRoom={openRoom}
        engineRef={engine}
        labelsRef={labels}
        onReady={onEngineReady}
        onAdvance={advanceSimulation}
      />
      {
        <div className="world-map-labels" aria-label={t('worldAgents')}>
          <div
            className="world-label-scene"
            ref={labels}
            style={{
              width: layout.width,
              height: layout.height,
            }}
          >
            {!room &&
              layout.rooms.map((entry) => {
                const spot = entry.spots.desk ?? entry.door
                return (
                  <div
                    key={entry.id}
                    className="world-map-label-group"
                    style={{ left: spot.x, top: spot.y }}
                  >
                    <button className="world-room-sign" onClick={() => onRoom(entry.id)}>
                      {t(entry.label)}
                    </button>
                  </div>
                )
              })}
            {Object.values(agents).map((agent) => {
              const event = latest.get(agent.id),
                behavior = event ? behaviorFor(event) : undefined,
                Status = icons[behavior?.icon ?? 'idle'],
                spot = event
                  ? destinationFor(event, layout)?.spot
                  : layout.rooms.find((room) => room.id === agent.homeRoomId)?.spots.desk
              return (
                <div
                  key={agent.id}
                  className="world-map-label-group"
                  data-world-agent={agent.id}
                  style={{ left: spot?.x ?? 0, top: spot?.y ?? 0 }}
                >
                  <button
                    className={`world-agent-hit ${agent.parentId ? 'world-helper-hit' : ''}`}
                    aria-label={t('worldAgentTask', {
                      agent: t(agent.name),
                      task: event
                        ? event.literal
                          ? event.text
                          : t(event.text)
                        : t('worldSeedIdle'),
                    })}
                    title={t('worldAgentTask', {
                      agent: t(agent.name),
                      task: event
                        ? event.literal
                          ? event.text
                          : t(event.text)
                        : t('worldSeedIdle'),
                    })}
                    onClick={() => onRoom(event?.target.roomId ?? agent.homeRoomId)}
                  />
                  <button
                    className={`world-agent-name ${agent.parentId ? 'world-helper-name' : ''}`}
                    title={t('worldAgentTask', {
                      agent: t(agent.name),
                      task: event
                        ? event.literal
                          ? event.text
                          : t(event.text)
                        : t('worldSeedIdle'),
                    })}
                    onClick={() => onRoom(event?.target.roomId ?? agent.homeRoomId)}
                  >
                    <Status size={15} />
                    {t(agent.name)}
                  </button>
                </div>
              )
            })}
          </div>
        </div>
      }
      <div className={`world-top-left ${missionsCollapsed ? 'is-collapsed' : ''}`}>
        {room && (
          <button className="world-back world-panel" onClick={() => onRoom(undefined)}>
            <ArrowLeft size={16} />
            {t('worldCampus')}
            <span>{t(room.label)}</span>
          </button>
        )}
        <MissionBoard
          missions={visibleMissions}
          agents={identities}
          collapsed={missionsCollapsed}
          onToggle={() => {
            setMissionsCollapsed(!missionsCollapsed)
            if (mobile) setActivityCollapsed(true)
          }}
        />
      </div>
      <ActivityFeed
        inline={false}
        entries={activity}
        agents={identities}
        roomLabels={Object.fromEntries(layout.rooms.map((room) => [room.id, room.label]))}
        roomId={room?.id}
        collapsed={activityCollapsed}
        unread={unread}
        onToggle={toggleActivity}
        onSelect={(event) => {
          if (event.target.roomId) onRoom(event.target.roomId)
          store.getState().reopen(event)
        }}
      />
      {room && (
        <>
          {room.deliverables
            .filter((item) => item.ready)
            .map((item) => (
              <button key={item.id} className="world-deliverable" onClick={() => setPreview(item)}>
                <BookOpen size={30} />
                <span>{t(item.title)}</span>
              </button>
            ))}
        </>
      )}
      {!room &&
        activity.some((event) => event.activity === 'deliver' && event.status === 'completed') && (
          <button
            style={{
              left: (layout.plaza.x - camera.x) * camera.zoom,
              top: (layout.plaza.y - camera.y) * camera.zoom,
            }}
            className="world-report"
            onClick={() => {
              const event = activity.find((event) => event.target.deliverableId)
              if (event?.target.roomId) onRoom(event.target.roomId)
            }}
          >
            <BookOpen size={30} />
            <span>{t('worldReport')}</span>
          </button>
        )}
      {preview && room?.deliverables.some((item) => item.id === preview.id) && (
        <div className="world-preview world-panel">
          <button
            className="world-preview-close"
            aria-label={t('close')}
            onClick={() => setPreview(null)}
          >
            <X size={15} />
          </button>
          <BookOpen size={28} />
          <strong>{t(preview.title)}</strong>
          <p>{t(preview.description)}</p>
          <button
            className="world-open"
            onClick={() => {
              if (preview.fileId) useFiles.getState().select(preview.fileId)
              onOpen(preview.agentId, preview.panel)
            }}
          >
            {t('worldOpen')}
          </button>
        </div>
      )}
      {!room && (
        <Minimap
          inline={false}
          width={layout.width}
          height={layout.height}
          background={layout.background}
          shapes={mapShapes}
          markers={markers}
          viewport={cameraViewport(camera, size, layout)}
          onNavigate={(point) => engine.current?.focusPoint(point, true)}
          onExpand={openMap}
        />
      )}
      <button
        className="world-map-launch world-panel"
        type="button"
        aria-label={t('worldOpenFullMap')}
        title={t('worldOpenFullMap')}
        onClick={openMap}
      >
        <MapIcon size={18} />
      </button>
      <MapModal
        open={mapOpen}
        width={layout.width}
        height={layout.height}
        background={layout.background}
        shapes={mapShapes}
        markers={markers}
        viewport={cameraViewport(camera, size, layout)}
        onClose={() => setMapOpen(false)}
        onRoom={onRoom}
        onNavigate={(point) => {
          if (room) {
            pendingMapFocus.current = point
            onRoom(undefined)
          } else engine.current?.focusPoint(point, true)
        }}
      />
      <WorldSettings
        speed={speed}
        zoom={zoom}
        onSpeed={(value) => {
          store.getState().setSpeed(value)
          if (new URLSearchParams(window.location.search).has('worldSpeed'))
            void navigate({
              to: '/agents/$agentId',
              params: { agentId },
              search: (previous) => ({
                ...previous,
                panel: previous.panel ?? 'files',
                worldSpeed: value,
              }),
              replace: true,
            })
        }}
        onZoom={(value) => engine.current?.setZoom(value)}
      />
      <div className="world-bottom" ref={bottom}>
        <DialogQueue messages={queue} agents={identities} onAdvance={store.getState().advance} />
        <form
          className="world-enter world-panel"
          onSubmit={(event) => {
            event.preventDefault()
            send()
          }}
        >
          <input
            ref={input}
            value={text}
            onChange={(event) => setText(event.target.value)}
            aria-label={t('worldTalk', { agent: t(selectedAgent?.name ?? 'worldUnknownAgent') })}
            placeholder={t('worldTalk', { agent: t(selectedAgent?.name ?? 'worldUnknownAgent') })}
          />
          <button type="submit" aria-label={t('send')}>
            <span className="world-enter-key">{t('worldEnterKey')}</span>
            <Send size={16} />
          </button>
          <button
            type="button"
            aria-label={t('microphone')}
            aria-pressed={microphone}
            onClick={() => setMicrophone(!microphone)}
          >
            <Mic size={17} />
          </button>
          {microphone && (
            <span className="world-mic-notice" role="status">
              {t('worldMicUnavailable')}
            </span>
          )}
        </form>
      </div>
    </div>
  )
}
