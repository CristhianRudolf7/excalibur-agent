import { describe, expect, it } from 'vitest'
import { layoutFlow, NODE_HEIGHT, NODE_WIDTH } from './layout'
import { linearAutomation, sampleAutomation } from './fixtures'
import type { AutomationStep } from '@/lib/rpc/contract'
describe('layered automation layout', () => {
  it('places a linear graph top to bottom after its trigger', () => {
    const result = layoutFlow(linearAutomation.steps, linearAutomation.edges)
    expect(result.nodes.map((node) => node.layer)).toEqual([0, 1, 2, 3])
    expect(new Set(result.nodes.map((node) => node.x)).size).toBe(1)
  })
  it('splits yes left and no right and rejoins at the longest path', () => {
    const { nodes } = layoutFlow(sampleAutomation.steps, sampleAutomation.edges)
    const find = (id: string) => nodes.find((node) => node.id === id)!
    expect(find('s3').layer).toBe(find('s4').layer)
    expect(find('s3').x).toBeLessThan(find('s4').x)
    expect(find('s5').layer).toBe(4)
    const uneven = layoutFlow(sampleAutomation.steps, [
      ...sampleAutomation.edges,
      { from: 's3', to: 's4' },
    ])
    expect(uneven.nodes.find((node) => node.id === 's5')?.layer).toBe(5)
  })
  it('keeps unreachable nodes inspectable and ignores invalid edges', () => {
    const result = layoutFlow(sampleAutomation.steps, [{ from: 's1', to: 'missing' }])
    expect(result.nodes).toHaveLength(6)
    expect(result.edges).toEqual([{ from: '__schedule', to: 's1' }])
    expect(result.nodes.every((node) => Number.isFinite(node.x + node.y))).toBe(true)
  })
  it('terminates on empty graphs and malformed cycles', () => {
    expect(layoutFlow([], []).nodes).toHaveLength(1)
    expect(
      layoutFlow(sampleAutomation.steps, [
        { from: 's1', to: 's2' },
        { from: 's2', to: 's1' },
      ]).nodes,
    ).toHaveLength(6)
  })
  it('has no overlaps for 30 steps in linear and wide graphs', () => {
    const steps: AutomationStep[] = Array.from({ length: 30 }, (_, index) => ({
      id: `s${index}`,
      title: `Step ${index}`,
      type: 'tool',
    }))
    for (const edges of [
      steps.slice(1).map((step) => ({ from: 's0', to: step.id })),
      steps.slice(1).map((step, index) => ({ from: `s${index}`, to: step.id })),
    ]) {
      const { nodes, width, height } = layoutFlow(steps, edges)
      for (const a of nodes) {
        expect(a.x + NODE_WIDTH).toBeLessThanOrEqual(width)
        expect(a.y + NODE_HEIGHT).toBeLessThanOrEqual(height)
        for (const b of nodes)
          if (a.id !== b.id)
            expect(Math.abs(a.x - b.x) >= NODE_WIDTH || Math.abs(a.y - b.y) >= NODE_HEIGHT).toBe(
              true,
            )
      }
    }
  })
})
