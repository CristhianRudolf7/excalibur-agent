/** Visual artifact contract implemented by the daemon's artifact event/RPC feature. */
export interface Artifact {
  id: string
  kind: 'diagram' | 'chart' | 'image' | 'document'
  title: string
  format: 'svg' | 'png' | 'pdf'
  dialect?: string
  group?: string
  mime: string
  size: number
  created_at: string
}
export interface ArtifactContent {
  id: string
  mime: string
  content: string
  source?: string
  dialect?: string
}
export interface TurnArtifact extends Artifact {
  turn_id: string
}
export interface ArtifactEvent {
  type: string
  payload: {
    session_id?: string
    turn_id?: string
    artifact?: Artifact
    artifacts?: Artifact[]
  }
}
