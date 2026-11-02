import { Children, isValidElement, type ReactElement, type ReactNode } from 'react'
import ReactMarkdown, { type Components } from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { ExternalLink } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { CodeBlock } from './CodeBlock'
import { CitationMarker, CitationProvider } from '@/features/library/Citations'
import { remarkCitations } from '@/features/library/citation-markers'
import { libraryEnabled } from '@/features/library/api'
import type { SourceRef } from '@/features/library/types'
import { safeMarkdownUrl } from './urls'

function align(value: unknown) {
  return value === 'right' || value === 'left' || value === 'center' ? value : undefined
}
function MarkdownTable({ children }: { children?: ReactNode }) {
  const { t } = useTranslation()
  return (
    <div
      className="markdown-table-scroll"
      role="region"
      aria-label={t('markdownTable')}
      tabIndex={0}
    >
      <table>{children}</table>
    </div>
  )
}
function MarkdownPre({ children }: { children?: ReactNode }) {
  const child = Children.toArray(children)[0]
  if (!isValidElement(child)) return <pre>{children}</pre>
  const code = child as ReactElement<{ children?: string; className?: string }>
  const language = /language-([\w+-]+)/.exec(code.props.className ?? '')?.[1] ?? ''
  return (
    <CodeBlock code={String(code.props.children ?? '').replace(/\n$/, '')} language={language} />
  )
}
// Stable component identities preserve code toolbar state while Markdown streams.
const components: Components = {
  sup: ({ node, children }) => (
    <CitationMarker n={Number(node?.properties['data-source-number'])}>{children}</CitationMarker>
  ),
  a: ({ href, children }) =>
    href ? (
      <a href={href} target="_blank" rel="noopener noreferrer">
        {children}
        <ExternalLink aria-hidden="true" size={12} />
      </a>
    ) : (
      <span>{children}</span>
    ),
  img: ({ src, alt, title }) =>
    typeof src === 'string' && src ? (
      <img src={src} alt={alt ?? ''} title={title} loading="lazy" />
    ) : (
      <span>{alt}</span>
    ),
  table: MarkdownTable,
  th: ({ node, children, ...props }) => (
    <th {...props} style={{ textAlign: align(node?.properties.align) }}>
      {children}
    </th>
  ),
  td: ({ node, children, ...props }) => (
    <td {...props} style={{ textAlign: align(node?.properties.align) }}>
      {children}
    </td>
  ),
  pre: MarkdownPre,
}
const emptySources: SourceRef[] = []
export default function MarkdownBody({
  children,
  sources = emptySources,
}: {
  children: string
  sources?: SourceRef[]
}) {
  return (
    <div className="markdown">
      <CitationProvider sources={sources}>
        <ReactMarkdown
          remarkPlugins={[
            remarkGfm,
            [remarkCitations, { sources: libraryEnabled ? sources : emptySources }],
          ]}
          urlTransform={safeMarkdownUrl}
          components={components}
        >
          {children}
        </ReactMarkdown>
      </CitationProvider>
    </div>
  )
}
