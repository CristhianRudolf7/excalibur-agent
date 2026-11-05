import { createHash } from 'node:crypto'
import casesJson from '../../../../internal/agents/testdata/avatar_cases.json?raw'
import { expect, it } from 'vitest'
import {
  avatarAccessories,
  avatarVariants,
  avatarSprite,
  fallbackAvatar,
  normalizeAvatarSpec,
  type AvatarSpec,
} from './avatar-spec'
import { portraitPixels, spritePixels } from './pixel-sprites'

const cases: { name: string; input: AvatarSpec; expected: AvatarSpec }[] = JSON.parse(casesJson)

it.each(cases)('matches server normalization: $name', ({ input, expected }) => {
  const original = structuredClone(input)
  expect(normalizeAvatarSpec(input)).toEqual(expected)
  expect(normalizeAvatarSpec(expected)).toEqual(expected)
  expect(input).toEqual(original)
})

it('falls back safely for every malformed field and absent spec', () => {
  for (const value of [undefined, null, [], '', 12, {}, { variant: 'hood' }]) {
    expect(normalizeAvatarSpec(value)).toEqual(fallbackAvatar)
  }
  for (const field of [
    'variant',
    'accessory',
    'tone',
    'hair',
    'highlight',
    'outfit',
    'accent',
    'background',
  ]) {
    for (const invalid of [
      undefined,
      null,
      42,
      '',
      'unknown',
      '#123',
      '#12345G',
      '#12345678',
      'red',
      'url(https://example.test)',
      'var(--skin)',
    ]) {
      expect(normalizeAvatarSpec({ ...fallbackAvatar, [field]: invalid })).toEqual(fallbackAvatar)
    }
  }
  const normalized = normalizeAvatarSpec(null)
  normalized.outfit = '#FFFFFF'
  expect(normalizeAvatarSpec(null).outfit).toBe(fallbackAvatar.outfit)
})

it.each(avatarVariants)(
  'keeps %s accessories across all poses without mutating the spec or previous grids',
  (variant) => {
    for (const accessory of avatarAccessories) {
      const spec = { ...fallbackAvatar, variant, accessory }
      const sprite = avatarSprite(spec)
      const before = structuredClone(spec)
      const first = portraitPixels(sprite)
      const copy = structuredClone(first)
      for (const action of ['idle', 'read', 'write', 'type', 'carry', 'think']) {
        for (let frame = 0; frame < 6; frame++) {
          const grid = spritePixels(sprite, frame, action)
          expect(grid).toHaveLength(28)
          expect(grid.every((row) => row.length === 16)).toBe(true)
          if (accessory === 'glasses' || accessory === 'sunglasses') {
            expect(grid[5]!.slice(5, 11).join('')).toBe(
              accessory === 'glasses' ? 'oooooo' : 'oloolo',
            )
          }
        }
      }
      expect(spec).toEqual(before)
      expect(first).toEqual(copy)
    }
  },
)

// Captured from the parent commit's primitives: portrait plus all six frames
// of idle, read, write, type, carry and think. Shared sprite poses stay intact.
const legacySignatures: Record<string, string> = {
  hood: '459343968df0889d94465ef50640203f046cfa84dc43043226cc350ffd472097',
  'long-hair': 'bf9d4c685b3ac7368f875e107cf0a866620fe51fac869542324cf141bd262414',
  cap: '6e7e061e1c2fe55590d90327aaa57fd70d97aa5d612768d97ac9a7e3d9458d70',
  bow: '89d7903a5b887ac2ece8bff85a9bb3ed657c6c8fff11170f128cee54834b9fa4',
  beard: 'f42726b7cf6aa12a928329635d958d864af5a93e6a815be144322e4272c55858',
  headphones: '357d2d302e38b91c60cf1a0574e618c1d4b6159144b17ba0cbc266eec37644fc',
}

it.each(avatarVariants)('preserves every legacy %s pixel grid', (variant) => {
  const sprite = { variant, tone: 'var(--primary)' }
  const grids = [
    portraitPixels(sprite),
    ...['idle', 'read', 'write', 'type', 'carry', 'think'].flatMap((action) =>
      Array.from({ length: 6 }, (_, frame) => spritePixels(sprite, frame, action)),
    ),
  ]
  expect(createHash('sha256').update(JSON.stringify(grids)).digest('hex')).toBe(
    legacySignatures[variant],
  )
})
