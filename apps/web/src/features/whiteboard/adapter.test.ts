import { expect, it } from 'vitest'
import { fromExcalidraw, toExcalidraw } from './adapter'
import type { Element, ElementType } from './types'
const types: ElementType[] = [
  'rect',
  'ellipse',
  'diamond',
  'line',
  'arrow',
  'text',
  'freedraw',
  'image',
  'frame',
  'math',
  'mermaid',
  'code',
  'sticky',
]
for (const type of types)
  it(`round trips ${type}, geometry, metadata, bindings and styles`, () => {
    const element: Element = {
      id: 'shape',
      type,
      x: 12,
      y: 24,
      width: 160,
      height: 80,
      text: 'Hello',
      latex: 'x^2',
      mermaid: 'graph TD; A-->B',
      code: 'const n = 1',
      language: 'js',
      src: '/blobs/test',
      author: 'agent',
      group_id: 'group',
      frame_id: 'frame',
      locked: true,
      points: [
        [0, 0],
        [160, 80],
      ],
      start_binding: { element_id: 'start', focus: 0, gap: 2 },
      end_binding: { element_id: 'end', focus: 0, gap: 2 },
      style: {
        stroke: 'rgb(1, 2, 3)',
        fill: 'transparent',
        stroke_width: 3,
        stroke_style: 'dashed',
        roughness: 2,
        opacity: 70,
        font_size: 24,
        font_family: '2',
        text_align: 'center',
      },
    }
    const restored = fromExcalidraw(toExcalidraw([element]))[0]!
    expect(restored).toMatchObject(element)
  })
it('preserves agent provenance while native user drawings get user provenance', () => {
  const native = toExcalidraw([{ id: 'new', type: 'rect', x: 0, y: 0, author: 'agent' }])[0]!
  expect(fromExcalidraw([{ ...native, customData: undefined }])[0]?.author).toBe('user')
  expect(fromExcalidraw([{ ...native, isDeleted: true }])).toEqual([])
})
