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
        opacity: 0.7,
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

it('renders native chart groups with solid fills, readable labels and normalized opacity', () => {
  const group: Element[] = [
    {
      id: 'c',
      type: 'rect',
      x: 40,
      y: 80,
      width: 560,
      height: 340,
      style: { fill: '#ffffff', stroke: '#cbd5e1', roughness: 0 },
      author: 'agent',
      group_id: 'c',
    },
    {
      id: 'c-title',
      type: 'text',
      x: 60,
      y: 94,
      width: 520,
      height: 27.5,
      text: 'Weekly expenses',
      style: { stroke: '#1e293b', font_size: 22, font_family: 'Helvetica' },
      author: 'agent',
      group_id: 'c',
    },
    {
      id: 'c-bar-0',
      type: 'rect',
      x: 144,
      y: 141,
      width: 300,
      height: 22,
      style: { fill: '#4f86e8', stroke: '#4f86e8', opacity: 0.8, roughness: 0 },
      author: 'agent',
      group_id: 'c',
    },
    {
      id: 'c-label-0',
      type: 'text',
      x: 56,
      y: 138,
      width: 80,
      height: 20,
      text: 'Food',
      style: { stroke: '#1e293b', font_size: 16 },
      author: 'agent',
      group_id: 'c',
    },
    {
      id: 'c-value-0',
      type: 'text',
      x: 454,
      y: 138,
      width: 100,
      height: 20,
      text: 'S/ 69.50',
      style: { stroke: '#1e293b', font_size: 16 },
      author: 'agent',
      group_id: 'c',
    },
    {
      id: 'c-axis',
      type: 'line',
      x: 144,
      y: 136,
      width: 0,
      height: 248,
      points: [
        [0, 0],
        [0, 248],
      ],
      style: { stroke: '#64748b', roughness: 0 },
      author: 'agent',
      group_id: 'c',
    },
  ]
  const native = toExcalidraw(group)
  expect(native.every((element) => element.groupIds[0] === 'c')).toBe(true)
  expect(native[0]).toMatchObject({ type: 'rectangle', backgroundColor: '#ffffff', opacity: 100 })
  expect(native[1]).toMatchObject({ type: 'text', fontSize: 22, text: 'Weekly expenses' })
  expect(native[2]).toMatchObject({
    type: 'rectangle',
    backgroundColor: '#4f86e8',
    fillStyle: 'solid',
    opacity: 80,
  })
  expect(native[3]).toMatchObject({
    type: 'text',
    text: 'Food',
    fontSize: 16,
    strokeColor: '#1e293b',
  })
  expect(native[4]).toMatchObject({ type: 'text', text: 'S/ 69.50', fontSize: 16 })
  expect(native[5]).toMatchObject({
    type: 'line',
    points: [
      [0, 0],
      [0, 248],
    ],
  })
  // Excalidraw handles light/dark canvas inversion. Preserve the explicit
  // foreground/background pair instead of depending on currentColor here.
  expect(fromExcalidraw(native)).toMatchObject(group)
})
it('preserves fully transparent native elements', () => {
  const input: Element = {
    id: 'hidden',
    type: 'rect',
    x: 0,
    y: 0,
    author: 'agent',
    style: { opacity: 0 },
  }
  expect(toExcalidraw([input])[0]?.opacity).toBe(0)
  expect(fromExcalidraw(toExcalidraw([input]))[0]?.style?.opacity).toBe(0)
})
