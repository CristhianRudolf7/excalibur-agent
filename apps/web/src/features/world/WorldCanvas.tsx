import { useEffect, useRef, useState, type RefObject } from 'react'
import { useTranslation } from 'react-i18next'
import { manualWorldClock } from './debug'
import { WorldEngine } from './engine/WorldEngine'
import { cameraScale } from './engine/camera'
import type { Viewport } from './minimap-transform'
import { updateMinimapViewport } from './minimap-dom'
import type { WorldStore } from './store'
/** React lifecycle adapter only; store input and room navigation callbacks are producer independent. */
export function WorldCanvas({
  store,
  roomId,
  onRoom,
  engineRef,
  labelsRef,
  onReady,
  onAdvance,
}: {
  store: WorldStore
  roomId?: string
  onRoom: (room: string) => void
  engineRef: RefObject<WorldEngine | null>
  labelsRef: RefObject<HTMLDivElement | null>
  onReady?: (engine: WorldEngine) => void
  onAdvance?: (milliseconds: number) => void
}) {
  const host = useRef<HTMLDivElement>(null),
    [error, setError] = useState(false)
  const { t } = useTranslation()
  useEffect(() => {
    let active = true
    if (host.current) delete host.current.dataset.worldReady
    const state = store.getState()
    const labelNodes = new Map<string, HTMLElement>()
    const markerNodes = new Map<string, SVGCircleElement>()
    let viewportNode: SVGRectElement | null = null
    let fullMap: HTMLDialogElement | null = null
    let fullViewport: SVGRectElement | null = null
    const fullMarkers = new Map<string, SVGCircleElement>()
    const frameViewport: Viewport = { x: 0, y: 0, width: 0, height: 0 }
    const engine = new WorldEngine({
      layout: state.layout,
      roomId,
      camera: { ...state.camera, zoom: state.zoom },
      speed: state.speed,
      manualClock: manualWorldClock(),
      onAdvance,
      getElapsed: () => store.getState().elapsed,
      onCamera: (camera) => {
        if (roomId) store.getState().setZoom(camera.zoom)
        else state.setCamera(camera)
      },
      onAgents: (agents) => {
        for (const id of labelNodes.keys()) if (!agents.has(id)) labelNodes.delete(id)
        for (const id of markerNodes.keys()) if (!agents.has(id)) markerNodes.delete(id)
        for (const id of fullMarkers.keys()) if (!agents.has(id)) fullMarkers.delete(id)
      },
      onTransform: (camera, agents) => {
        const scene = labelsRef.current
        if (!scene) return
        const room = store.getState().layout.rooms.find((room) => room.id === roomId)
        const width = host.current?.clientWidth ?? 0,
          height = host.current?.clientHeight ?? 0
        const scale = cameraScale(camera.zoom, width, height, room)
        scene.style.setProperty('--world-label-scale', String(1 / scale))
        scene.style.transform = `translate(${-camera.x * scale}px, ${-camera.y * scale}px) scale(${scale})`
        const layout = store.getState().layout,
          markerScale = 220 / layout.width
        if (!room && host.current) {
          if (!viewportNode?.isConnected)
            viewportNode =
              host.current.parentElement?.querySelector<SVGRectElement>('.world-map-viewport') ??
              null
          if (viewportNode) {
            frameViewport.x = camera.x
            frameViewport.y = camera.y
            frameViewport.width = width / camera.zoom
            frameViewport.height = height / camera.zoom
            updateMinimapViewport(viewportNode, frameViewport, layout)
          }
        }
        if (!fullMap?.isConnected)
          fullMap =
            host.current?.parentElement?.querySelector<HTMLDialogElement>('.world-map-modal') ??
            null
        if (fullMap?.open && !room) {
          fullViewport ??= fullMap.querySelector<SVGRectElement>('.world-full-map-viewport')
          if (fullViewport) {
            frameViewport.x = camera.x
            frameViewport.y = camera.y
            frameViewport.width = width / camera.zoom
            frameViewport.height = height / camera.zoom
            updateMinimapViewport(
              fullViewport,
              frameViewport,
              layout,
              Number(fullViewport.dataset.mapStroke) || 1.5,
            )
          }
        }
        for (const [id, actor] of agents) {
          let node = labelNodes.get(id)
          if (!node?.isConnected) {
            node =
              scene.querySelector<HTMLElement>(`[data-world-agent="${CSS.escape(id)}"]`) ??
              undefined
            if (node) labelNodes.set(id, node)
          }
          if (node) {
            node.style.display = room && actor.roomId !== room.id ? 'none' : ''
            node.style.left = `${actor.position.x}px`
            node.style.top = `${actor.position.y}px`
          }
          let marker = markerNodes.get(id)
          if (!marker?.isConnected) {
            marker =
              host.current?.parentElement?.querySelector<SVGCircleElement>(
                `[data-world-marker="${CSS.escape(id)}"]`,
              ) ?? undefined
            if (marker) markerNodes.set(id, marker)
          }
          if (fullMap?.open && !room) {
            let fullMarker = fullMarkers.get(id)
            if (!fullMarker?.isConnected) {
              fullMarker =
                fullMap.querySelector<SVGCircleElement>(
                  `[data-world-full-marker="${CSS.escape(id)}"]`,
                ) ?? undefined
              if (fullMarker) fullMarkers.set(id, fullMarker)
            }
            if (fullMarker) {
              fullMarker.setAttribute('cx', String(actor.position.x * markerScale))
              fullMarker.setAttribute('cy', String(actor.position.y * markerScale))
            }
          }
          if (marker) {
            marker.setAttribute('cx', String(actor.position.x * markerScale))
            marker.setAttribute('cy', String(actor.position.y * markerScale))
          }
        }
      },
    })
    engineRef.current = engine
    for (const agent of Object.values(state.agents)) engine.upsertAgent(agent)
    // Restore current behavior without replaying historical spawn/remove operations.
    const restored = new Set<string>()
    for (const event of state.activity)
      if (!restored.has(event.agentId)) {
        restored.add(event.agentId)
        engine.applyEvent({ ...event, agentChanges: undefined })
      }
    const click = engine.onRoomClick(onRoom)
    const unsubscribe = store.subscribe((next, previous) => {
      if (next.speed !== previous.speed) engine.setSpeed(next.speed)
      if (next.zoom !== previous.zoom && Math.abs(next.zoom - engine.camera.zoom) > 0.0001)
        engine.setZoom(next.zoom)
      if (next.layout !== previous.layout) void engine.setLayout(next.layout)
      for (const [id, agent] of Object.entries(next.agents))
        if (agent !== previous.agents[id]) engine.upsertAgent(agent)
      for (const id of Object.keys(previous.agents)) if (!next.agents[id]) engine.removeAgent(id)
      if (next.activity !== previous.activity && next.activity[0])
        engine.applyEvent(next.activity[0])
    })
    void engine
      .mount(host.current!)
      .then(() => {
        if (active && host.current) {
          host.current.dataset.worldReady = 'true'
          onReady?.(engine)
        }
      })
      .catch(() => {
        if (active) setError(true)
        engine.unmount()
      })
    return () => {
      active = false
      unsubscribe()
      click()
      engine.unmount()
      if (engineRef.current === engine) engineRef.current = null
    }
  }, [store, roomId, onRoom, engineRef, labelsRef, onReady, onAdvance])
  return (
    <div className="world-canvas" ref={host} aria-label={t('worldCampus')}>
      {error && <p className="world-render-error">{t('worldRenderError')}</p>}
    </div>
  )
}
