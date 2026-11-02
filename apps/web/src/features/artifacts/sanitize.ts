const namespace = 'http://www.w3.org/2000/svg'
const tags = new Set([
  'svg',
  'g',
  'defs',
  'symbol',
  'use',
  'path',
  'rect',
  'circle',
  'ellipse',
  'line',
  'polyline',
  'polygon',
  'text',
  'tspan',
  'textPath',
  'title',
  'desc',
  'clipPath',
  'mask',
  'marker',
  'linearGradient',
  'radialGradient',
  'stop',
  'pattern',
  'style',
])
const attributes = new Set([
  'id',
  'class',
  'xmlns',
  'viewBox',
  'width',
  'height',
  'x',
  'y',
  'x1',
  'x2',
  'y1',
  'y2',
  'cx',
  'cy',
  'r',
  'rx',
  'ry',
  'd',
  'points',
  'transform',
  'preserveAspectRatio',
  'dx',
  'dy',
  'rotate',
  'textLength',
  'lengthAdjust',
  'startOffset',
  'offset',
  'gradientUnits',
  'gradientTransform',
  'spreadMethod',
  'fx',
  'fy',
  'patternUnits',
  'patternContentUnits',
  'patternTransform',
  'clipPathUnits',
  'maskUnits',
  'maskContentUnits',
  'markerWidth',
  'markerHeight',
  'refX',
  'refY',
  'orient',
  'markerUnits',
  'role',
  'aria-label',
])
const presentation = new Set([
  'fill',
  'fill-opacity',
  'fill-rule',
  'stroke',
  'stroke-width',
  'stroke-opacity',
  'stroke-dasharray',
  'stroke-dashoffset',
  'stroke-linecap',
  'stroke-linejoin',
  'stroke-miterlimit',
  'opacity',
  'color',
  'font-family',
  'font-size',
  'font-weight',
  'font-style',
  'text-anchor',
  'text-decoration',
  'dominant-baseline',
  'alignment-baseline',
  'letter-spacing',
  'word-spacing',
  'white-space',
  'clip-path',
  'clip-rule',
  'mask',
  'marker-start',
  'marker-mid',
  'marker-end',
  'stop-color',
  'stop-opacity',
  'display',
  'visibility',
  'vector-effect',
  'paint-order',
  'shape-rendering',
  'text-rendering',
])
const fragment = /^#[\w.:-]+$/
function safeValue(value: string) {
  // Only local paint/marker references are allowed, including in CSS declarations.
  const withoutLocal = value.replace(/url\(\s*['"]?#[\w.:-]+['"]?\s*\)/gi, '')
  return (
    ![...value].some((character) => character.charCodeAt(0) < 32) &&
    !/[\\@<>]|url\s*\(|expression\s*\(|javascript:|https?:|data:/i.test(withoutLocal)
  )
}
function declarations(css: string) {
  return css
    .split(';')
    .flatMap((entry) => {
      const colon = entry.indexOf(':')
      const property = entry.slice(0, colon).trim().toLowerCase()
      const value = entry.slice(colon + 1).trim()
      return colon > 0 && presentation.has(property) && safeValue(value)
        ? [`${property}:${value}`]
        : []
    })
    .join(';')
}
function stylesheet(css: string) {
  // Keep ordinary diagram class rules. At-rules, escapes and nested rules are discarded.
  if (/[\\@<>]|\/\*/.test(css)) return ''
  return [...css.matchAll(/([^{}]+)\{([^{}]*)\}/g)]
    .flatMap(([, selector, body]) => {
      const safe = declarations(body!)
      return /^[\w\s.#,:>+~*[\]="'()-]+$/.test(selector!) && safe ? [`${selector}{${safe}}`] : []
    })
    .join('\n')
}
/** Strict SVG subset, then displayed in an isolated image document via a blob URL. */
export function sanitizeSVG(source: string) {
  const document = new DOMParser().parseFromString(source, 'image/svg+xml')
  const root = document.documentElement
  if (
    document.querySelector('parsererror') ||
    root.localName !== 'svg' ||
    root.namespaceURI !== namespace ||
    document.doctype
  )
    throw new Error('Invalid SVG')
  const visit = (element: Element) => {
    for (const attribute of [...element.attributes]) {
      const name = attribute.name
      if (name === 'xmlns' && attribute.value === namespace) continue
      if (attribute.localName === 'href') {
        if (!fragment.test(attribute.value)) element.removeAttributeNode(attribute)
      } else if (name === 'style') {
        element.setAttribute('style', declarations(attribute.value))
      } else if (!(attributes.has(name) || presentation.has(name)) || !safeValue(attribute.value)) {
        element.removeAttributeNode(attribute)
      }
    }
    if (element.localName === 'style') element.textContent = stylesheet(element.textContent ?? '')
    for (const child of [...element.childNodes]) {
      if (child.nodeType === Node.ELEMENT_NODE) {
        const nested = child as Element
        if (nested.namespaceURI !== namespace || !tags.has(nested.localName)) nested.remove()
        else visit(nested)
      } else if (child.nodeType !== Node.TEXT_NODE) child.remove()
    }
  }
  visit(root)
  root.setAttributeNS('http://www.w3.org/2000/xmlns/', 'xmlns', namespace)
  return new XMLSerializer().serializeToString(root)
}
