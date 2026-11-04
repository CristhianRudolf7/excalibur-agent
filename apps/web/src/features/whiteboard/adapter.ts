import type { ExcalidrawElement } from '@excalidraw/excalidraw/element/types'
import type { Element } from './types'

const fontID = (family?: string) =>
  Number(family) || ({ Virgil: 1, Helvetica: 2, Cascadia: 3, Excalifont: 5 }[family ?? ''] ?? 2)
const nativeType = (type: Element['type']) =>
  type === 'rect' || type === 'sticky'
    ? 'rectangle'
    : ['math', 'mermaid', 'code'].includes(type)
      ? 'image'
      : type
export function toExcalidraw(elements: readonly Element[]): ExcalidrawElement[] {
  return elements.map((element) => ({
    seed: 1,
    version: 1,
    versionNonce: 0,
    isDeleted: false,
    boundElements: null,
    updated: 1,
    link: null,
    index: null,
    roundness: null,
    containerId: null,
    verticalAlign: 'top',
    autoResize: false,
    name: null,
    id: element.id,
    type: nativeType(element.type),
    x: element.x,
    y: element.y,
    width: element.width ?? 180,
    height: element.height ?? 100,
    angle: 0,
    strokeColor: element.style?.stroke ?? 'currentColor',
    backgroundColor: element.style?.fill ?? 'transparent',
    fillStyle: 'solid',
    strokeWidth: element.style?.stroke_width ?? 2,
    strokeStyle: element.style?.stroke_style ?? 'solid',
    roughness: element.style?.roughness ?? 1,
    opacity: (element.style?.opacity ?? 1) * 100,
    fontSize: element.style?.font_size ?? 20,
    fontFamily: fontID(element.style?.font_family),
    textAlign: element.style?.text_align ?? 'left',
    text: element.text ?? '',
    originalText: element.text ?? '',
    lineHeight: 1.25,
    points: element.points ?? [
      [0, 0],
      [element.width ?? 180, element.height ?? 100],
    ],
    pressures: [],
    simulatePressure: true,
    startBinding: element.start_binding
      ? {
          elementId: element.start_binding.element_id,
          focus: element.start_binding.focus ?? 0,
          gap: element.start_binding.gap ?? 0,
        }
      : null,
    endBinding: element.end_binding
      ? {
          elementId: element.end_binding.element_id,
          focus: element.end_binding.focus ?? 0,
          gap: element.end_binding.gap ?? 0,
        }
      : null,
    endArrowhead: element.type === 'arrow' ? 'arrow' : null,
    startArrowhead: null,
    groupIds: element.group_id ? [element.group_id] : [],
    frameId: element.frame_id ?? null,
    locked: element.locked ?? false,
    fileId: element.id,
    status: 'saved',
    scale: [1, 1],
    customData: { contract: element },
  })) as unknown as ExcalidrawElement[]
}
export function fromExcalidraw(elements: readonly ExcalidrawElement[]): Element[] {
  return elements
    .filter((element) => !element.isDeleted)
    .map((element) => {
      const original = element.customData?.contract as Element | undefined
      const type =
        original?.type ??
        (element.type === 'rectangle' || element.type === 'selection'
          ? 'rect'
          : element.type === 'magicframe'
            ? 'frame'
            : element.type === 'embeddable' || element.type === 'iframe'
              ? 'image'
              : element.type)
      const binding = (value: { elementId: string; focus: number; gap: number } | null) =>
        value ? { element_id: value.elementId, focus: value.focus, gap: value.gap } : null
      return {
        ...original,
        id: element.id,
        type,
        x: element.x,
        y: element.y,
        width: element.width,
        height: element.height,
        ...('points' in element ? { points: element.points } : {}),
        ...('text' in element ? { text: element.text } : {}),
        ...('startBinding' in element
          ? {
              start_binding: binding(element.startBinding),
              end_binding: binding(element.endBinding),
            }
          : {}),
        style: {
          ...original?.style,
          stroke: element.strokeColor,
          fill: element.backgroundColor,
          stroke_width: element.strokeWidth,
          stroke_style: element.strokeStyle,
          roughness: element.roughness,
          opacity: element.opacity / 100,
          ...('fontSize' in element
            ? {
                font_size: element.fontSize,
                font_family:
                  original?.style?.font_family &&
                  fontID(original.style.font_family) === element.fontFamily
                    ? original.style.font_family
                    : String(element.fontFamily),
                text_align: element.textAlign as NonNullable<Element['style']>['text_align'],
              }
            : {}),
        },
        group_id: element.groupIds[0],
        frame_id: element.frameId ?? undefined,
        locked: element.locked,
        author: original?.author ?? 'user',
      }
    })
}
