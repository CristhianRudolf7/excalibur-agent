import type { SpriteDefinition } from './pixel-sprites'

export const avatarVariants = ['hood', 'long-hair', 'cap', 'bow', 'beard', 'headphones'] as const
export const avatarAccessories = [
  'none',
  'glasses',
  'sunglasses',
  'earring',
  'scarf',
  'star',
] as const

/** Matches the personal-agent AvatarSpec contract. Tone always means skin. */
export interface AvatarSpec {
  /** Web presentation preset; generated avatars retain the daemon's palette contract. */
  preset?: 'paladin'
  variant: (typeof avatarVariants)[number]
  tone: string
  hair: string
  highlight: string
  outfit: string
  accent: string
  accessory: (typeof avatarAccessories)[number]
  background: string
}

export const fallbackAvatar: Readonly<AvatarSpec> = Object.freeze({
  variant: 'hood',
  tone: '#F2C9A1',
  hair: '#30283F',
  highlight: '#D5E2FF',
  outfit: '#586FA8',
  accent: '#F4C76B',
  accessory: 'none',
  background: '#E6ECF8',
})

export const paladinAvatar: Readonly<AvatarSpec> = Object.freeze({
  ...fallbackAvatar,
  preset: 'paladin',
})

const colorFields = ['tone', 'hair', 'highlight', 'outfit', 'accent', 'background'] as const
const hexColor = /^#[0-9a-fA-F]{6}$/

function contrast(a: string, b: string): number {
  const luminance = (color: string) => {
    const channels = [1, 3, 5].map((i) => {
      const c = parseInt(color.slice(i, i + 2), 16) / 255
      return c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4
    })
    return 0.2126 * channels[0]! + 0.7152 * channels[1]! + 0.0722 * channels[2]!
  }
  const la = luminance(a),
    lb = luminance(b)
  return (Math.max(la, lb) + 0.05) / (Math.min(la, lb) + 0.05)
}

/** Untrusted persisted or RPC data gets the same contrast rules as the server. */
export function normalizeAvatarSpec(value: unknown): AvatarSpec {
  if (!value || typeof value !== 'object') return { ...fallbackAvatar }
  const data = value as Record<string, unknown>
  if (
    !avatarVariants.some((v) => v === data.variant) ||
    !avatarAccessories.some((v) => v === data.accessory) ||
    colorFields.some((field) => typeof data[field] !== 'string' || !hexColor.test(data[field]))
  ) {
    return { ...fallbackAvatar }
  }
  const spec = {
    ...fallbackAvatar,
    ...(data.preset === 'paladin' ? { preset: 'paladin' as const } : {}),
    variant: data.variant as AvatarSpec['variant'],
    accessory: data.accessory as AvatarSpec['accessory'],
  }
  for (const field of colorFields) spec[field] = (data[field] as string).toUpperCase()
  if (contrast(spec.tone, '#171923') < 3) spec.tone = fallbackAvatar.tone
  if (contrast(spec.hair, spec.tone) < 1.5) {
    spec.hair = '#30283F'
    if (contrast(spec.hair, spec.tone) < 1.5) spec.hair = '#FFF1D6'
  }
  return spec
}

/** Keeps the existing pixel-sprite meaning of tone (clothing) intact. */
export function avatarSprite(spec: AvatarSpec): SpriteDefinition {
  return { ...spec, tone: spec.outfit, skin: spec.tone }
}
