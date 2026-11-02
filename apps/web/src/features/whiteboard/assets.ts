import katex from 'katex'
import { common, createLowlight } from 'lowlight'
import { convertToExcalidrawElements, restoreElements } from '@excalidraw/excalidraw'
import type { BinaryFiles } from '@excalidraw/excalidraw/types'
import type { ExcalidrawElement } from '@excalidraw/excalidraw/element/types'
import { toExcalidraw } from './adapter'
import type { Element } from './types'
const lowlight = createLowlight(common)
const escape = (text: string) =>
  text
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
function highlighted(code: string, language?: string): string {
  const tree =
    language && lowlight.registered(language)
      ? lowlight.highlight(language, code)
      : lowlight.highlightAuto(code)
  const walk = (node: (typeof tree.children)[number]): string =>
    node.type === 'text'
      ? escape(node.value)
      : node.type === 'element'
        ? `<span class="${escape(String(node.properties.className ?? ''))}">${node.children.map((child) => walk(child as typeof node)).join('')}</span>`
        : ''
  return tree.children.map(walk).join('')
}
export async function renderScene(elements: Element[]) {
  const tokens = getComputedStyle(document.documentElement)
  const foreground = tokens.getPropertyValue('--foreground').trim() || 'currentColor'
  const primary = tokens.getPropertyValue('--primary').trim() || foreground
  const secondary = tokens.getPropertyValue('--success').trim() || foreground
  const files: BinaryFiles = {}
  const result: ExcalidrawElement[] = []
  for (const element of [...elements].sort((a, b) => (a.z ?? 0) - (b.z ?? 0))) {
    if (element.type === 'mermaid') {
      try {
        const { parseMermaidToExcalidraw } = await import('@excalidraw/mermaid-to-excalidraw')
        const parsed = await parseMermaidToExcalidraw(element.mermaid ?? '')
        const children = convertToExcalidrawElements(parsed.elements)
        const names = new Map(children.map((child, index) => [child.id, `${element.id}:${index}`]))
        const rename = (id: string) => names.get(id) ?? id
        const minX = Math.min(...children.map((child) => child.x)),
          minY = Math.min(...children.map((child) => child.y))
        result.push(
          ...children.map((child, index) => ({
            ...child,
            id: `${element.id}:${index}`,
            x: element.x + child.x - minX,
            y: element.y + child.y - minY,
            groupIds: [element.id],
            frameId: child.frameId ? rename(child.frameId) : null,
            boundElements:
              child.boundElements?.map((binding) => ({ ...binding, id: rename(binding.id) })) ??
              null,
            ...('containerId' in child
              ? { containerId: child.containerId ? rename(child.containerId) : null }
              : {}),
            ...('startBinding' in child
              ? {
                  startBinding: child.startBinding
                    ? { ...child.startBinding, elementId: rename(child.startBinding.elementId) }
                    : null,
                  endBinding: child.endBinding
                    ? { ...child.endBinding, elementId: rename(child.endBinding.elementId) }
                    : null,
                }
              : {}),
            customData: { mermaidParent: element.id },
          })),
        )
        Object.assign(files, parsed.files)
        continue
      } catch {
        /* Invalid diagrams remain editable source text. */
      }
    }
    if (element.type === 'math' || element.type === 'code' || element.type === 'mermaid') {
      const width = element.width ?? 420,
        height = element.height ?? 180
      let body: string
      if (element.type === 'math')
        body = katex.renderToString(element.latex ?? '', {
          throwOnError: false,
          output: 'mathml',
          trust: false,
        })
      else
        body = `<pre style="white-space:pre-wrap">${highlighted(element.code ?? element.mermaid ?? '', element.language)}</pre>`
      const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="${width}" height="${height}"><foreignObject width="100%" height="100%"><div xmlns="http://www.w3.org/1999/xhtml" style="padding:12px;font:16px monospace;color:${escape(element.style?.stroke ?? foreground)}"><style>.hljs-keyword,.hljs-built_in{font-weight:bold;color:${escape(primary)}}.hljs-string{color:${escape(secondary)}}.hljs-comment{opacity:.65;font-style:italic}</style>${body}</div></foreignObject></svg>`
      files[element.id] = {
        id: element.id as never,
        dataURL: `data:image/svg+xml,${encodeURIComponent(svg)}` as never,
        mimeType: 'image/svg+xml',
        created: Date.now(),
      }
    } else if (element.type === 'image' && element.src) {
      if (element.src.startsWith('data:image/'))
        files[element.id] = {
          id: element.id as never,
          dataURL: element.src as never,
          mimeType: element.src.split(';')[0]!.slice(5) as BinaryFiles[string]['mimeType'],
          created: Date.now(),
        }
      else if (
        element.src.startsWith('/blobs/') ||
        /^[a-f0-9]{64}$/.test(element.src) ||
        /^https?:\/\//.test(element.src)
      ) {
        const source = /^[a-f0-9]{64}$/.test(element.src) ? `/blobs/${element.src}` : element.src
        const response = await fetch(source, {
          credentials: source.startsWith('/blobs/') ? 'same-origin' : 'omit',
        })
        if (!response.ok) throw new Error('Image unavailable')
        const blob = await response.blob()
        const dataURL = await new Promise<string>((resolve, reject) => {
          const reader = new FileReader()
          reader.onload = () => resolve(String(reader.result))
          reader.onerror = reject
          reader.readAsDataURL(blob)
        })
        files[element.id] = {
          id: element.id as never,
          dataURL: dataURL as never,
          mimeType: blob.type as BinaryFiles[string]['mimeType'],
          created: Date.now(),
        }
      }
    }
    result.push(...toExcalidraw([element]))
    if (element.type === 'sticky' && element.text)
      result.push(
        ...toExcalidraw([
          {
            ...element,
            id: `${element.id}:label`,
            type: 'text',
            x: element.x + 12,
            y: element.y + 12,
            width: Math.max(20, (element.width ?? 180) - 24),
          },
        ]).map((child) => ({ ...child, customData: { contract: element, stickyLabel: true } })),
      )
  }
  return {
    elements: restoreElements(
      result.map((element) => ({
        ...element,
        strokeColor: element.strokeColor === 'currentColor' ? foreground : element.strokeColor,
      })),
      null,
    ),
    files,
  }
}
