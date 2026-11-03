import { PixelPortrait, PixelSprite } from './PixelSprite'
import { avatarSprite, normalizeAvatarSpec, type AvatarSpec } from './avatar-spec'

export type { AvatarSpec } from './avatar-spec'

export interface AgentAvatarProps {
  spec: AvatarSpec
  /** Square portrait size, or full sprite height, in CSS pixels. */
  size?: number
  form?: 'portrait' | 'sprite'
  frame?: number
  /** Caller supplies the localized agent name; unlabeled avatars are decorative. */
  label?: string
}

/** Personal-agent avatar with a safe fallback for malformed stored specs. */
export function AgentAvatar({
  spec,
  size = 40,
  form = 'portrait',
  frame = 0,
  label,
}: AgentAvatarProps) {
  const sprite = avatarSprite(normalizeAvatarSpec(spec))
  const Renderer = form === 'sprite' ? PixelSprite : PixelPortrait
  return <Renderer sprite={sprite} size={size} frame={frame} label={label} />
}
