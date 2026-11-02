import type { Board, Element, Op, BoardEvent } from './types'
export function applyOps(elements: Element[], ops: Op[]): Element[] {
  let scene = new Map(elements.map((element) => [element.id, element]))
  for (const op of ops) {
    if (op.op === 'clear' || op.op === 'set_scene') scene = new Map()
    if (op.op === 'remove') for (const id of op.ids ?? []) scene.delete(id)
    for (const element of op.elements ?? []) {
      if (op.op === 'update' && !scene.has(element.id)) continue
      scene.set(element.id, { ...scene.get(element.id), ...element } as Element)
    }
  }
  return [...scene.values()]
}
export function diffScene(before: Element[], after: Element[]): Op[] {
  const previous = new Map(before.map((element) => [element.id, element]))
  const ids = new Set(after.map((element) => element.id))
  const removed = before.filter((element) => !ids.has(element.id)).map((element) => element.id)
  const changed = after.filter(
    (element) => JSON.stringify(previous.get(element.id)) !== JSON.stringify(element),
  )
  return [
    ...(removed.length ? [{ op: 'remove' as const, ids: removed }] : []),
    ...(changed.length ? [{ op: 'add' as const, elements: changed }] : []),
  ]
}
/** Serializes delivery and reconciles missing versions before consuming buffered events. */
export class VersionBuffer {
  private pending = new Map<number, BoardEvent>()
  private chain = Promise.resolve()
  private read: () => Board
  private write: (board: Board) => void
  private fetch: () => Promise<Board>
  private render: (event: BoardEvent) => Promise<void>
  constructor(
    read: () => Board,
    write: (board: Board) => void,
    fetch: () => Promise<Board>,
    render: (event: BoardEvent) => Promise<void>,
  ) {
    this.read = read
    this.write = write
    this.fetch = fetch
    this.render = render
  }
  receive(event: BoardEvent) {
    if (event.version === undefined || event.version <= this.read().version) return this.chain
    this.pending.set(event.version, event)
    this.chain = this.chain
      .catch(() => {})
      .then(async () => {
        while (this.pending.size) {
          const board = this.read()
          for (const version of this.pending.keys())
            if (version <= board.version) this.pending.delete(version)
          if (!this.pending.size) break
          const next = this.pending.get(board.version + 1)
          if (!next) {
            const latest = await this.fetch()
            this.write(latest)
            if (latest.version <= board.version) throw new Error('Unresolved version gap')
            continue
          }
          this.pending.delete(board.version + 1)
          await this.render(next)
          const current = this.read()
          this.write({
            ...current,
            elements: applyOps(current.elements, next.ops ?? []),
            version: next.version!,
          })
        }
      })
    return this.chain
  }
}
/** Cancellation prevents a closed board from continuing a scripted reveal. */
export async function scheduleElements(
  ops: Op[],
  step: number,
  reduced: boolean,
  render: (ops: Op[]) => void | Promise<void>,
  signal: AbortSignal,
) {
  for (const op of ops) {
    if (signal.aborted) return
    if (reduced || !op.elements?.length || op.op !== 'add') {
      await render([op])
      continue
    }
    for (const element of op.elements) {
      if (signal.aborted) return
      await render([{ op: 'add', elements: [element] }])
      await new Promise<void>((resolve) => {
        const done = () => {
          clearTimeout(timer)
          signal.removeEventListener('abort', done)
          resolve()
        }
        const timer = setTimeout(done, Math.min(2000, Math.max(16, step)))
        signal.addEventListener('abort', done, { once: true })
      })
    }
  }
}

export function revealFrames(element: Element): Element[] {
  return Array.from({ length: 6 }, (_, index) => {
    const progress = (index + 1) / 6
    if (progress === 1) return element
    if (element.type === 'text')
      return {
        ...element,
        text: (element.text ?? '').slice(0, Math.ceil((element.text?.length ?? 0) * progress)),
      }
    if (element.type === 'freedraw')
      return {
        ...element,
        points: element.points?.slice(0, Math.max(2, Math.ceil(element.points.length * progress))),
      }
    if (element.type === 'arrow' || element.type === 'line')
      return {
        ...element,
        points: element.points?.map(([x, y]) => [x * progress, y * progress] as const),
      }
    const scale = 0.96 + 0.04 * progress
    const width = element.width ?? 180,
      height = element.height ?? 100
    return {
      ...element,
      x: element.x + (width * (1 - scale)) / 2,
      y: element.y + (height * (1 - scale)) / 2,
      width: width * scale,
      height: height * scale,
      style: { ...element.style, opacity: (element.style?.opacity ?? 100) * progress },
    }
  })
}
