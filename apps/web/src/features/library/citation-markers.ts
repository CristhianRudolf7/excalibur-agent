import type { SourceRef } from './types'
interface MarkdownNode {
  type: string
  value?: string
  children?: MarkdownNode[]
  data?: { hName: string; hProperties: Record<string, string> }
}
/** Transform only prose text nodes; code, links and definitions keep their literal content. */
export function remarkCitations({ sources }: { sources: SourceRef[] }) {
  const numbers = new Set(sources.map((source) => source.n))
  return (tree: MarkdownNode) => {
    const visit = (node: MarkdownNode) => {
      if (
        [
          'code',
          'inlineCode',
          'link',
          'linkReference',
          'definition',
          'html',
          'image',
          'imageReference',
        ].includes(node.type)
      )
        return
      if (!node.children) return
      node.children = node.children.flatMap((child) => {
        if (child.type !== 'text' || !child.value) {
          visit(child)
          return [child]
        }
        const output: MarkdownNode[] = []
        let offset = 0
        for (const match of child.value.matchAll(/\[(\d+)\]/g)) {
          const n = Number(match[1])
          if (!numbers.has(n)) continue
          const index = match.index
          if (index > offset) output.push({ type: 'text', value: child.value.slice(offset, index) })
          output.push({
            type: 'libraryCitation',
            data: { hName: 'sup', hProperties: { 'data-source-number': String(n) } },
            children: [{ type: 'text', value: match[0] }],
          })
          offset = index + match[0].length
        }
        if (offset < child.value.length)
          output.push({ type: 'text', value: child.value.slice(offset) })
        return output
      })
    }
    visit(tree)
  }
}
