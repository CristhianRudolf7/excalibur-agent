import { Application, Container, Graphics, Sprite, Texture, TextureStyle } from 'pixi.js'
import { spritePixels, pixelColors } from '../sprites'
import type { WorldLayout, Room } from '../layout'
import { cameraScale, type Camera } from './camera'
import type { AgentState, WorldRenderer } from './types'
export class PixiRenderer implements WorldRenderer {
  private app = new Application()
  private scene = new Container()
  private background = new Container()
  private actors = new Container()
  private outline = new Graphics()
  private sprites = new Map<string, { sprite: Sprite; frames: Texture[]; signature: string }>()
  private host?: HTMLElement
  private mounted = false
  private disposed = false
  private generation = 0
  private room?: Room
  async mount(host: HTMLElement, tick: (delta: number, actualElapsed?: number) => void) {
    this.host = host
    await this.app.init({
      width: host.clientWidth || 800,
      height: host.clientHeight || 600,
      resolution: Math.min(window.devicePixelRatio || 1, 2),
      roundPixels: true,
      autoDensity: true,
      autoStart: false,
      sharedTicker: false,
      antialias: false,
      preference: 'webgl',
      powerPreference: 'low-power',
    })
    if (this.disposed) {
      this.app.destroy(true)
      return
    }
    this.mounted = true
    TextureStyle.defaultOptions.scaleMode = 'nearest'
    this.scene.addChild(this.background, this.actors, this.outline)
    this.app.stage.addChild(this.scene)
    this.app.ticker.maxFPS = 30
    this.app.ticker.add((ticker) => tick(ticker.deltaMS, ticker.elapsedMS))
    host.prepend(this.app.canvas)
    this.app.canvas.setAttribute('role', 'img')
  }
  private color(token: string) {
    return getComputedStyle(this.host!).getPropertyValue(token).trim()
  }
  async setLayout(layout: WorldLayout, room?: Room) {
    if (!this.mounted || this.disposed) return
    const generation = ++this.generation
    this.room = room
    const url = room ? (room.background ?? layout.fallbackBackground) : layout.background
    let image: HTMLImageElement | undefined
    if (url) {
      image = new Image()
      image.src = url
      try {
        await image.decode()
      } catch {
        image = undefined
      }
    }
    if (generation !== this.generation || this.disposed) return
    this.background
      .removeChildren()
      .forEach((child) => child.destroy({ children: true, texture: true, textureSource: true }))
    const size = room?.interiorSize ?? layout
    if (image) {
      const texture = Texture.from(image)
      // Preserve authored pixels: integer zoom duplicates texels instead of smoothing them.
      texture.source.resolution = 1
      texture.source.autoGenerateMipmaps = false
      texture.source.scaleMode = 'nearest'
      const sprite = new Sprite(texture)
      sprite.width = size.width
      sprite.height = size.height
      this.background.addChild(sprite)
    } else
      this.background.addChild(
        new Graphics().rect(0, 0, size.width, size.height).fill(this.color('--world-room-floor')),
      )
    if (!room)
      for (const entry of layout.rooms)
        if (entry.decorate) {
          const polygon = entry.polygon.flatMap((point) => [point.x, point.y])
          const floor = new Graphics()
            .poly(polygon)
            .fill({ color: this.color('--world-room-floor'), alpha: 0.95 })
            .stroke({ color: this.color('--world-room-wall'), width: 16 })
          const desk = entry.spots.desk ?? entry.door
          floor
            .rect(desk.x - 65, desk.y - 35, 130, 42)
            .fill(this.color('--world-desk'))
            .stroke({ color: this.color('--world-room-wall'), width: 4 })
          this.background.addChild(floor)
        }
  }
  upsertAgent(state: AgentState) {
    if (!this.mounted || this.disposed) return
    const signature = JSON.stringify([state.agent.sprite, state.animation])
    if (this.sprites.get(state.agent.id)?.signature !== signature) this.removeAgent(state.agent.id)
    if (!this.sprites.has(state.agent.id)) {
      const definition = state.agent.sprite
      const probe = document.createElement('span')
      probe.style.setProperty('--sprite-tone', definition.tone)
      probe.style.setProperty('--sprite-hair', definition.hair ?? definition.tone)
      probe.style.setProperty(
        '--sprite-highlight',
        definition.highlight ?? 'color-mix(in srgb, var(--sprite-tone) 55%, var(--surface))',
      )
      this.host!.append(probe)
      const palette: Record<string, string> = {}
      for (const [key, value] of Object.entries(pixelColors)) {
        probe.style.color = value
        palette[key] = getComputedStyle(probe).color
      }
      const frames = Array.from({ length: 6 }, (_, frame) => {
        const canvas = document.createElement('canvas')
        canvas.width = 16
        canvas.height = 28
        const ctx = canvas.getContext('2d')!
        spritePixels(definition, frame, state.animation).forEach((row, y) =>
          row.forEach((pixel, x) => {
            if (pixel !== '.') {
              ctx.fillStyle = palette[pixel]!
              ctx.fillRect(x, y, 1, 1)
            }
          }),
        )
        const texture = Texture.from(canvas)
        texture.source.scaleMode = 'nearest'
        return texture
      })
      probe.remove()
      const sprite = new Sprite(frames[0])
      sprite.anchor.set(0.5, 1)
      sprite.roundPixels = true
      sprite.scale.set(
        (this.room ? 5 : 4.5) * (definition.scale ?? (state.agent.parentId ? 0.65 : 1)),
      )
      this.actors.addChild(sprite)
      this.sprites.set(state.agent.id, { sprite, frames, signature })
    }
    this.moveAgent(state)
  }
  removeAgent(id: string) {
    const item = this.sprites.get(id)
    if (!item) return
    item.sprite.destroy()
    for (const frame of item.frames) frame.destroy(true)
    this.sprites.delete(id)
  }
  moveAgent(state: AgentState) {
    const item = this.sprites.get(state.agent.id)
    if (!item) return
    item.sprite.visible = !this.room || state.roomId === this.room.id
    item.sprite.position.set(state.position.x, state.position.y)
    item.sprite.texture = item.frames[state.frame]!
  }
  setCamera(camera: Camera, room?: Room) {
    if (!this.host) return
    const scale = cameraScale(camera.zoom, this.host.clientWidth, this.host.clientHeight, room)
    this.scene.scale.set(scale)
    this.scene.position.set(-camera.x * scale, -camera.y * scale)
  }

  highlight(room?: Room) {
    this.outline.clear()
    if (room)
      this.outline
        .poly(room.polygon.flatMap((point) => [point.x, point.y]))
        .fill({ color: this.color('--primary'), alpha: 0.15 })
        .stroke({ color: this.color('--primary'), width: 3 })
  }
  render() {
    if (this.mounted && !this.disposed) this.app.render()
  }
  start() {
    if (this.mounted && !this.disposed) this.app.start()
  }
  stop() {
    if (this.mounted && !this.disposed) this.app.stop()
  }
  resize(width: number, height: number) {
    if (this.mounted && !this.disposed) this.app.renderer.resize(width, height)
  }
  unmount() {
    this.disposed = true
    this.generation++
    for (const id of this.sprites.keys()) this.removeAgent(id)
    if (this.mounted) this.app.destroy(true, { children: true, texture: true, textureSource: true })
    this.mounted = false
  }
}
