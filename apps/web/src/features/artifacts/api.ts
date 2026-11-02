import { rpc } from '@/lib/rpc/client'
import { sanitizeSVG } from './sanitize'
import type { Artifact } from './types'

export async function loadArtifact(artifact: Artifact, signal?: AbortSignal) {
  const content = await rpc.call('artifacts.get', { id: artifact.id }, { signal })
  const mime = { svg: 'image/svg+xml', png: 'image/png', pdf: 'application/pdf' }[artifact.format]
  if (content.id !== artifact.id || content.mime !== mime)
    throw new Error('Invalid artifact content')
  const svg = artifact.format === 'svg' ? sanitizeSVG(content.content) : undefined
  const bytes =
    svg ?? Uint8Array.from(atob(content.content), (character) => character.charCodeAt(0))
  return { ...content, svg, blob: new Blob([bytes], { type: mime }) }
}
