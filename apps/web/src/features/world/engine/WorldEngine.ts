import { activityMap, behaviorFor, destinationFor, type ActivityMap } from '../activity-map'
import { publishWorldDebug } from '../debug'
import type { WorldAgent, WorldEvent } from '../events'
import { roomAt, type Point, type Room, type WorldLayout } from '../layout'
import {
  clampCamera,
  stepZoom,
  snapZoom,
  clampZoom,
  zoomCamera,
  cameraScale,
  type Camera,
} from './camera'
import { PixiRenderer } from './PixiRenderer'
import { TickerController } from './ticker'
import { normalizeSpeed } from '../preferences'
import type { AgentState, WorldRenderer } from './types'
export interface EngineOptions {
  layout: WorldLayout
  activities?: ActivityMap
  renderer?: WorldRenderer
  camera?: Camera
  roomId?: string
  speed?: number
  manualClock?: boolean
  onAdvance?: (milliseconds: number) => void
  getElapsed?: () => number
  onCamera?: (camera: Camera) => void
  onAgents?: (agents: ReadonlyMap<string, AgentState>) => void
  onTransform?: (camera: Camera, agents: ReadonlyMap<string, AgentState>) => void
}
/** Standalone engine. Producers only call applyEvent; renderers and framework adapters can be replaced independently. */
export class WorldEngine {
  readonly agents = new Map<string, AgentState>()
  readonly renderer: WorldRenderer
  readonly ticker: TickerController
  layout: WorldLayout
  camera: Camera
  private roomId?: string
  private host?: HTMLElement
  private mounted = false
  private ready = false
  private disposed = false
  private loading = 0
  private cleanups: (() => void)[] = []
  private clicks = new Set<(roomId: string) => void>()
  private pointers = new Map<number, Point>()
  private dragging = false
  private moved = false
  private origin: Point = { x: 0, y: 0 }
  private pinch = 0
  private pinchZoom = 1
  private pinching = false
  private speed = 1
  private frameMilliseconds = 0
  private frameCount = 0
  private measuredFps = 0
  private lastDemandFrame = 0
  private pan?: Point
  private elapsed = 0
  private idleFrame = 0
  private clickTimer?: ReturnType<typeof setTimeout>
  private reduced = false
  private options: EngineOptions
  constructor(options: EngineOptions) {
    this.options = options
    this.layout = options.layout
    this.camera = { ...(options.camera ?? { x: 0, y: 0, zoom: 0.75 }) }
    this.camera.zoom = snapZoom(this.camera.zoom)
    this.roomId = options.roomId
    this.speed = normalizeSpeed(options.speed ?? 1)
    this.renderer = options.renderer ?? new PixiRenderer()
    this.ticker = new TickerController(
      () => {
        if (!this.options.manualClock) this.renderer.start()
      },
      () => this.renderer.stop(),
      (running) => {
        this.frameMilliseconds = 0
        this.frameCount = 0
        if (running) this.measuredFps = 0
        this.lastDemandFrame = performance.now()
        this.publishDebug()
      },
    )
  }
  get room(): Room | undefined {
    return this.layout.rooms.find((room) => room.id === this.roomId)
  }
  async mount(host: HTMLElement) {
    this.host = host
    this.disposed = false
    this.ready = false
    this.publishDebug()
    const media = window.matchMedia('(prefers-reduced-motion: reduce)')
    this.reduced = media.matches
    this.ticker.reducedMotion = this.reduced
    await this.renderer.mount(host, (delta, elapsed) => {
      if (!this.options.manualClock) this.tick(delta, elapsed)
    })
    if (this.disposed) return
    this.mounted = true
    this.lastDemandFrame = performance.now()
    if (this.room) {
      const size = this.bounds,
        scale = this.scale
      this.camera.x = (size.width - host.clientWidth / scale) / 2
      this.camera.y = (size.height - host.clientHeight / scale) / 2
    }
    await this.setLayout(this.layout)
    if (this.disposed) return
    this.applyCamera(true)
    const resize = new ResizeObserver(() => {
      if (!this.mounted) return
      this.renderer.resize(host.clientWidth, host.clientHeight)
      this.applyCamera(true)
    })
    resize.observe(host)
    let intersecting = true
    const visibility = () => {
      this.ticker.visible = intersecting && !document.hidden
      this.ticker.update()
      this.publishDebug()
      this.render()
    }
    const observer = new IntersectionObserver(([entry]) => {
      intersecting = !!entry?.isIntersecting
      visibility()
    })
    observer.observe(host)
    const motion = () => {
      this.reduced = media.matches
      this.ticker.reducedMotion = this.reduced
      if (this.reduced) this.finishMotion()
      this.updateMotion()
    }
    this.listen(document, 'visibilitychange', visibility)
    media.addEventListener('change', motion)
    this.cleanups.push(() => {
      resize.disconnect()
      observer.disconnect()
      media.removeEventListener('change', motion)
    })
    this.listen(host, 'pointerdown', this.down)
    this.listen(host, 'pointermove', this.move)
    this.listen(host, 'pointerup', this.up)
    this.listen(host, 'pointercancel', this.cancel)
    this.listen(host, 'wheel', this.wheel, { passive: false })
    this.listen(host, 'dblclick', this.doubleClick)
    const idle = setInterval(() => {
      if (
        !this.mounted ||
        this.options.manualClock ||
        !this.ticker.visible ||
        document.hidden ||
        this.ticker.running ||
        this.reduced
      )
        return
      this.idleFrame = 1 - this.idleFrame
      for (const state of this.agents.values()) {
        state.frame = this.idleFrame
        this.renderer.moveAgent(state)
      }
      this.render()
      const now = performance.now()
      this.measuredFps = 1000 / Math.max(1, now - this.lastDemandFrame)
      this.lastDemandFrame = now
      this.publishDebug()
    }, 1800)
    this.cleanups.push(() => clearInterval(idle))
    this.ready = true
    this.publishDebug()
    this.updateMotion()
  }
  unmount() {
    this.disposed = true
    this.mounted = false
    this.ready = false
    this.loading++
    clearTimeout(this.clickTimer)
    for (const cleanup of this.cleanups.splice(0)) cleanup()
    this.ticker.destroy()
    this.renderer.unmount()
    this.host = undefined
    publishWorldDebug({ running: false, fps: 0, mode: 'chat', ready: false })
  }
  async setLayout(layout: WorldLayout) {
    this.layout = layout
    if (!this.mounted) return
    const generation = ++this.loading
    await this.renderer.setLayout(layout, this.room)
    if (this.disposed || generation !== this.loading) return
    for (const state of this.agents.values()) this.renderer.upsertAgent(state)
    this.applyCamera(true)
  }
  upsertAgent(agent: WorldAgent) {
    const previous = this.agents.get(agent.id)
    const room =
      this.layout.rooms.find((room) => room.id === agent.homeRoomId) ?? this.layout.rooms[0]
    if (!room) return
    const sibling = [...this.agents.values()].filter(
      (state) => state.agent.homeRoomId === agent.homeRoomId,
    ).length
    const base = this.room ? (room.interiorSpots.desk ?? room.door) : (room.spots.desk ?? room.door)
    const position = {
      x: base.x + (agent.parentId ? 65 + sibling * 30 : 0),
      y: base.y + (agent.parentId ? 55 : 0),
    }
    const state: AgentState = previous
      ? { ...previous, agent }
      : {
          agent,
          position,
          roomId: room.id,
          route: [],
          waypoint: 0,
          animation: 'idle',
          icon: 'idle',
          task: '',
          frame: 0,
        }
    this.agents.set(agent.id, state)
    this.renderer.upsertAgent(state)
    this.options.onAgents?.(this.agents)
    this.options.onTransform?.(this.camera, this.agents)
    this.render()
  }
  removeAgent(id: string) {
    this.agents.delete(id)
    this.renderer.removeAgent(id)
    this.options.onAgents?.(this.agents)
    this.options.onTransform?.(this.camera, this.agents)
    this.render()
  }
  applyEvent(event: WorldEvent) {
    for (const agent of event.agentChanges?.upsert ?? []) this.upsertAgent(agent)
    for (const id of event.agentChanges?.remove ?? []) this.removeAgent(id)
    const state = this.agents.get(event.agentId),
      destination = destinationFor(event, this.layout, this.options.activities ?? activityMap)
    if (!state || !destination) return
    const behavior = behaviorFor(event, this.options.activities ?? activityMap)
    const spot = this.room
      ? (destination.room.interiorSpots[behavior.spot] ??
        destination.room.interiorSpots.desk ??
        destination.spot)
      : destination.spot
    state.roomId = destination.room.id
    state.animation = behavior.animation
    state.icon = behavior.icon
    state.task = event.text
    state.route = this.room
      ? [spot]
      : [
          ...(Math.hypot(state.position.x - spot.x, state.position.y - spot.y) > 200
            ? destination.room.waypoints
            : []),
          destination.room.door,
          spot,
        ]
    state.waypoint = 0
    if (this.reduced || !this.mounted) {
      state.position.x = spot.x
      state.position.y = spot.y
      state.waypoint = state.route.length
    }
    this.renderer.upsertAgent(state)
    this.options.onAgents?.(this.agents)
    this.options.onTransform?.(this.camera, this.agents)
    this.updateMotion()
    this.render()
  }
  focusRoom(id: string) {
    const room = this.layout.rooms.find((room) => room.id === id)
    if (room) this.focusPoint(room.spots.desk ?? room.door)
  }
  focusPoint(point: Point, immediate = false) {
    if (this.room || this.dragging || !this.host) return
    const target = clampCamera(
      {
        ...point,
        x: point.x - this.host.clientWidth / this.camera.zoom / 2,
        y: point.y - this.host.clientHeight / this.camera.zoom / 2,
        zoom: this.camera.zoom,
      },
      this.host.clientWidth,
      this.host.clientHeight,
      this.layout,
    )
    this.pan = target
    if (this.reduced || immediate) {
      this.camera.x = target.x
      this.camera.y = target.y
      this.pan = undefined
      this.applyCamera(true)
    }
    this.updateMotion()
  }
  onRoomClick(listener: (roomId: string) => void) {
    this.clicks.add(listener)
    return () => this.clicks.delete(listener)
  }
  hitTest(point: Point) {
    return roomAt(this.layout, point)
  }
  private listen(
    target: EventTarget,
    type: string,
    callback: EventListener,
    options?: AddEventListenerOptions,
  ) {
    target.addEventListener(type, callback, options)
    this.cleanups.push(() => target.removeEventListener(type, callback, options))
  }
  private render() {
    if (this.mounted && this.ticker.visible && !document.hidden) this.renderer.render()
  }
  private get bounds() {
    return this.room?.interiorSize ?? (this.room ? { width: 1600, height: 1000 } : this.layout)
  }
  private get scale() {
    return cameraScale(
      this.camera.zoom,
      this.host?.clientWidth ?? 0,
      this.host?.clientHeight ?? 0,
      this.room,
    )
  }
  /** Shared by settings, wheel and pinch. Center is retained before clamping to the current scene. */
  setZoom(value: number, snap = true) {
    const zoom = snap ? snapZoom(value) : clampZoom(value)
    if (!this.host) {
      this.camera.zoom = zoom
      return
    }
    this.pan = undefined
    this.camera = zoomCamera(
      this.camera,
      zoom,
      { width: this.host.clientWidth, height: this.host.clientHeight },
      this.bounds,
      this.room,
    )
    this.applyCamera(snap)
  }
  setSpeed(value: number) {
    this.speed = normalizeSpeed(value)
  }
  private applyCamera(notify: boolean) {
    if (!this.host) return
    this.camera = clampCamera(
      this.camera,
      this.host.clientWidth,
      this.host.clientHeight,
      this.bounds,
      this.scale,
    )
    this.renderer.setCamera(this.camera, this.room)
    this.options.onTransform?.(this.camera, this.agents)
    if (notify) this.options.onCamera?.({ ...this.camera })
    this.render()
    this.publishDebug()
  }
  private hasPoseAnimation() {
    for (const state of this.agents.values())
      if (
        (!this.room || state.roomId === this.room.id) &&
        state.animation !== 'idle' &&
        state.animation !== 'wait'
      )
        return true
    return false
  }
  private publishDebug() {
    const running =
      this.mounted &&
      this.ticker.visible &&
      !document.hidden &&
      !this.reduced &&
      (this.ticker.running || this.hasPoseAnimation())
    publishWorldDebug({
      running,
      fps: running ? this.measuredFps : 0,
      mode: this.room ? 'room' : 'campus',
      ready: this.ready,
      camera: { ...this.camera },
      elapsed: this.options.getElapsed?.() ?? this.elapsed,
      ...(this.ready ? { advance: this.advance, settle: this.settle } : {}),
    })
  }
  /** Advance the same simulation used by the renderer without relying on browser frames. */
  advance = (milliseconds: number) => {
    if (!this.ready || !Number.isFinite(milliseconds) || milliseconds < 0) return
    let remaining = milliseconds
    while (remaining > 0) {
      const delta = Math.min(16, remaining)
      this.options.onAdvance?.(delta)
      this.tick(delta)
      remaining -= delta
    }
    this.render()
    this.publishDebug()
  }
  /** Finish camera interpolation only; agent routes and event time remain untouched. */
  settle = () => {
    if (!this.ready) return
    if (this.pan) {
      this.camera.x = this.pan.x
      this.camera.y = this.pan.y
      this.pan = undefined
      this.applyCamera(true)
    }
    this.updateMotion()
  }
  private finishMotion() {
    for (const state of this.agents.values()) {
      const end = state.route.at(-1)
      if (end) {
        state.position.x = end.x
        state.position.y = end.y
      }
      state.waypoint = state.route.length
      state.frame = 0
      this.renderer.moveAgent(state)
    }
    if (this.pan) {
      this.camera.x = this.pan.x
      this.camera.y = this.pan.y
      this.pan = undefined
      this.applyCamera(true)
    }
    this.options.onTransform?.(this.camera, this.agents)
  }
  private updateMotion() {
    if (!this.mounted) return
    this.ticker.moving =
      !!this.pan || [...this.agents.values()].some((state) => state.waypoint < state.route.length)
    this.ticker.update()
    this.publishDebug()
  }
  private tick = (delta: number, actualElapsed = delta) => {
    this.frameMilliseconds += actualElapsed
    this.frameCount++
    if (this.frameMilliseconds >= 400 || this.measuredFps === 0) {
      this.measuredFps = Math.min(30, (this.frameCount * 1000) / this.frameMilliseconds)
      if (this.frameMilliseconds >= 400) {
        this.frameCount = 0
        this.frameMilliseconds = 0
      }
      this.publishDebug()
    }
    this.elapsed += delta * this.speed
    let moving = false
    if (this.pan) {
      const rate = Math.min(1, delta / 110)
      this.camera.x += (this.pan.x - this.camera.x) * rate
      this.camera.y += (this.pan.y - this.camera.y) * rate
      if (Math.abs(this.pan.x - this.camera.x) + Math.abs(this.pan.y - this.camera.y) < 1) {
        this.camera.x = this.pan.x
        this.camera.y = this.pan.y
        this.pan = undefined
        this.options.onCamera?.({ ...this.camera })
      }
      this.renderer.setCamera(this.camera, this.room)
    }
    for (const state of this.agents.values()) {
      const target = state.route[state.waypoint]
      if (!target) continue
      const dx = target.x - state.position.x,
        dy = target.y - state.position.y,
        distance = Math.hypot(dx, dy)
      if (distance <= 2) {
        state.position.x = target.x
        state.position.y = target.y
        state.waypoint++
        state.frame = 0
      } else {
        const step = Math.min(distance, delta * 0.16 * this.speed)
        state.position.x += (dx / distance) * step
        state.position.y += (dy / distance) * step
        state.frame = 2 + (Math.floor(this.elapsed / 160) % 4)
      }
      if (state.waypoint < state.route.length) moving = true
      this.renderer.moveAgent(state)
    }
    this.options.onTransform?.(this.camera, this.agents)
    this.ticker.moving = moving || !!this.pan
    this.ticker.update()
    this.publishDebug()
  }
  private point(event: MouseEvent): Point {
    const rect = this.host!.getBoundingClientRect()
    return {
      x: (event.clientX - rect.left) / this.scale + this.camera.x,
      y: (event.clientY - rect.top) / this.scale + this.camera.y,
    }
  }
  private down: EventListener = (source) => {
    const event = source as PointerEvent
    this.host!.setPointerCapture?.(event.pointerId)
    this.pointers.set(event.pointerId, { x: event.clientX, y: event.clientY })
    this.origin.x = event.clientX
    this.origin.y = event.clientY
    this.dragging = true
    this.moved = false
    this.pan = undefined
    if (this.pointers.size === 1) {
      this.pinch = 0
      this.pinching = false
    }
    if (this.pointers.size === 2) {
      const [a, b] = [...this.pointers.values()]
      this.pinch = Math.hypot(a!.x - b!.x, a!.y - b!.y)
      this.pinchZoom = this.camera.zoom
      this.pinching = true
      this.moved = true
    }
  }
  private move: EventListener = (source) => {
    const event = source as PointerEvent
    if (!this.dragging) {
      if (!this.room) this.renderer.highlight(this.hitTest(this.point(event)))
      this.render()
      return
    }
    this.pointers.set(event.pointerId, { x: event.clientX, y: event.clientY })
    if (this.pointers.size === 2) {
      const [a, b] = [...this.pointers.values()],
        distance = Math.hypot(a!.x - b!.x, a!.y - b!.y)
      if (this.pinch) this.setZoom((this.pinchZoom * distance) / this.pinch, false)
      this.moved = true
    } else {
      const dx = event.clientX - this.origin.x,
        dy = event.clientY - this.origin.y
      this.camera.x -= dx / this.scale
      this.camera.y -= dy / this.scale
      if (Math.abs(dx) + Math.abs(dy) > 2) this.moved = true
    }
    this.origin.x = event.clientX
    this.origin.y = event.clientY
    this.applyCamera(!this.pinching)
  }
  private up: EventListener = (source) => {
    const event = source as PointerEvent
    this.pointers.delete(event.pointerId)
    if (this.pinching && this.pointers.size < 2) {
      this.setZoom(this.camera.zoom)
      this.pinching = false
      this.moved = true
      const remaining = this.pointers.values().next().value
      if (remaining) {
        this.origin.x = remaining.x
        this.origin.y = remaining.y
      }
    }
    if (!this.moved && !this.room) {
      const room = this.hitTest(this.point(event))
      if (room) {
        clearTimeout(this.clickTimer)
        this.clickTimer = setTimeout(() => {
          for (const listener of this.clicks) listener(room.id)
        }, 220)
      }
    }
    this.dragging = this.pointers.size > 0
  }
  private cancel: EventListener = () => {
    if (this.pinching) this.setZoom(this.camera.zoom)
    this.pinching = false
    this.dragging = false
    this.pointers.clear()
  }
  private wheel: EventListener = (source) => {
    const event = source as WheelEvent
    event.preventDefault()
    this.pan = undefined
    if (event.deltaY) this.setZoom(stepZoom(this.camera.zoom, -event.deltaY))
  }
  private doubleClick: EventListener = (source) => {
    if (this.room) return
    clearTimeout(this.clickTimer)
    const room = this.hitTest(this.point(source as MouseEvent))
    if (room) this.focusRoom(room.id)
  }
}
