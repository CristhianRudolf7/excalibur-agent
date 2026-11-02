import type { AutomationEdge, AutomationStep } from '@/lib/rpc/contract'

export const NODE_WIDTH = 240
export const NODE_HEIGHT = 126
const GAP_X = 48
const GAP_Y = 74
const MARGIN = 30
export interface FlowNode {
  id: string
  x: number
  y: number
  layer: number
}
export interface FlowLayout {
  nodes: FlowNode[]
  width: number
  height: number
  edges: AutomationEdge[]
}

/** Longest paths on the reachable DAG. Invalid edges and cycles cannot stall layout. */
export function layoutFlow(steps: AutomationStep[], edges: AutomationEdge[]): FlowLayout {
  const ids = new Set(steps.map((step) => step.id))
  const valid = edges.filter(
    (edge) => ids.has(edge.from) && ids.has(edge.to) && edge.from !== edge.to,
  )
  const outgoing = (id: string) =>
    valid
      .filter((edge) => edge.from === id)
      .sort((a, b) => (a.label === 'no' ? 1 : 0) - (b.label === 'no' ? 1 : 0))
  const reachable = new Set<string>()
  const order: string[] = []
  const visit = (id: string) => {
    if (reachable.has(id)) return
    reachable.add(id)
    order.push(id)
    outgoing(id).forEach((edge) => visit(edge.to))
  }
  if (steps[0]) visit(steps[0].id)
  const indegree = new Map(
    order.map((id) => [
      id,
      valid.filter((edge) => edge.to === id && reachable.has(edge.from)).length,
    ]),
  )
  const layers = new Map<string, number>()
  const queue = order.filter((id) => indegree.get(id) === 0)
  queue.forEach((id) => layers.set(id, 1))
  while (queue.length) {
    const id = queue.shift()!
    for (const edge of outgoing(id)) {
      layers.set(edge.to, Math.max(layers.get(edge.to) ?? 1, (layers.get(id) ?? 1) + 1))
      const count = (indegree.get(edge.to) ?? 1) - 1
      indegree.set(edge.to, count)
      if (count === 0) queue.push(edge.to)
    }
  }
  // Malformed cyclic or disconnected nodes remain inspectable below the reachable graph.
  let tail = Math.max(0, ...layers.values()) + 1
  for (const step of steps) if (!layers.has(step.id)) layers.set(step.id, tail++)
  const rows = new Map<number, string[]>()
  for (const id of [...order, ...steps.map((step) => step.id).filter((id) => !reachable.has(id))]) {
    const layer = layers.get(id)!
    const row = rows.get(layer) ?? []
    row.push(id)
    rows.set(layer, row)
  }
  const columns = Math.max(1, ...[...rows.values()].map((row) => row.length))
  const width = MARGIN * 2 + columns * NODE_WIDTH + (columns - 1) * GAP_X
  const nodes: FlowNode[] = [{ id: '__schedule', x: (width - NODE_WIDTH) / 2, y: MARGIN, layer: 0 }]
  for (const [layer, row] of rows) {
    const rowWidth = row.length * NODE_WIDTH + (row.length - 1) * GAP_X
    row.forEach((id, index) =>
      nodes.push({
        id,
        layer,
        x: (width - rowWidth) / 2 + index * (NODE_WIDTH + GAP_X),
        y: MARGIN + layer * (NODE_HEIGHT + GAP_Y),
      }),
    )
  }
  nodes.sort((a, b) => a.layer - b.layer || a.x - b.x)
  const first = steps[0]
  return {
    nodes,
    width,
    height: MARGIN * 2 + (Math.max(0, ...layers.values()) + 1) * (NODE_HEIGHT + GAP_Y) - GAP_Y,
    edges: [
      ...(first ? [{ from: '__schedule', to: first.id }] : []),
      ...valid.filter((edge) => (layers.get(edge.to) ?? 0) > (layers.get(edge.from) ?? 0)),
    ],
  }
}

export function edgePath(from: FlowNode, to: FlowNode) {
  const x1 = from.x + NODE_WIDTH / 2
  const y1 = from.y + NODE_HEIGHT
  const x2 = to.x + NODE_WIDTH / 2
  const y2 = to.y
  const mid = (y1 + y2) / 2
  return `M ${x1} ${y1} C ${x1} ${mid}, ${x2} ${mid}, ${x2} ${y2 - 6}`
}
