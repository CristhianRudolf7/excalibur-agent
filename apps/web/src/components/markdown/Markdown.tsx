import { lazy, Suspense } from 'react'
import './markdown.css'
import type { SourceRef } from '@/features/library/types'

const MarkdownBody = lazy(() => import('./MarkdownBody'))
/** Reusable, HTML-free Markdown for chat, documents and activity surfaces. */
export function Markdown({ children, sources }: { children: string; sources?: SourceRef[] }) {
  return (
    <Suspense fallback={<div className="markdown markdown-pending">{children}</div>}>
      <MarkdownBody sources={sources}>{children}</MarkdownBody>
    </Suspense>
  )
}
