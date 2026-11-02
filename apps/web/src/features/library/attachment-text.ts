import type { DocumentInfo } from './types'

export function attachmentText(text: string, documents: DocumentInfo[], label = 'Attached') {
  return [
    text.trim(),
    ...documents.map(
      (document) => `${label}: ${document.title || document.source_name} [document:${document.id}]`,
    ),
  ]
    .filter(Boolean)
    .join('\n')
}
