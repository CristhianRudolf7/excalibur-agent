import { createLowlight } from 'lowlight'
import javascript from 'highlight.js/lib/languages/javascript'
import typescript from 'highlight.js/lib/languages/typescript'
import json from 'highlight.js/lib/languages/json'
import bash from 'highlight.js/lib/languages/bash'
import diff from 'highlight.js/lib/languages/diff'
import css from 'highlight.js/lib/languages/css'
import type { ReactNode } from 'react'

const light = createLowlight({ javascript, typescript, json, bash, diff, css })
type HighlightNode = ReturnType<typeof light.highlight>['children'][number]
function token(node: HighlightNode, key: number): ReactNode {
  if (node.type === 'text') return node.value
  if (node.type !== 'element') return null
  const names = node.properties.className
  return (
    <span key={key} className={Array.isArray(names) ? names.join(' ') : undefined}>
      {node.children.map(token)}
    </span>
  )
}
/** HAST is rendered as React text/spans, never injected as raw HTML. */
export default function HighlightedCode({ code, language }: { code: string; language: string }) {
  return (
    <code className="hljs">
      {light.registered(language) ? light.highlight(language, code).children.map(token) : code}
    </code>
  )
}
