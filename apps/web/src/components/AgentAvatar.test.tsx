import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import { AgentAvatar } from './AgentAvatar'
import { AgentAvatarGallery } from './AgentAvatar.gallery'
import { avatarAccessories, avatarVariants, avatarSprite, fallbackAvatar } from './avatar-spec'
import { portraitPixels, spritePixels } from './pixel-sprites'

afterEach(cleanup)

it('renders a gallery of every variant and accessory in both forms', () => {
  render(<AgentAvatarGallery sizes={[40]} />)
  const images = screen.getAllByRole('img')
  expect(images).toHaveLength(72)
  const byLabel = new Map(images.map((image) => [image.getAttribute('aria-label'), image]))
  for (const variant of avatarVariants) {
    for (const accessory of avatarAccessories) {
      for (const form of ['portrait', 'sprite']) {
        const avatar = byLabel.get(`${variant} / ${accessory} / ${form} / 40`)!
        expect(avatar.querySelector('svg')).toHaveAttribute(
          'viewBox',
          form === 'portrait' ? '0 0 14 14' : '0 0 16 28',
        )
        expect(avatar.querySelector('svg')).toHaveAttribute('shape-rendering', 'crispEdges')
        expect(avatar.querySelectorAll('rect').length).toBeGreaterThan(100)
      }
    }
  }
})

it.each(avatarVariants)(
  'renders six distinct accessory overlays for %s in each form',
  (variant) => {
    for (const pixels of [portraitPixels, spritePixels]) {
      const signatures = avatarAccessories.map((accessory) => {
        const grid = pixels(avatarSprite({ ...fallbackAvatar, variant, accessory }))
        expect(grid.every((row) => row.length === grid[0]!.length)).toBe(true)
        expect(grid.flat().every((pixel) => '.ohlfsetag'.includes(pixel))).toBe(true)
        return JSON.stringify(grid)
      })
      expect(new Set(signatures).size).toBe(6)
    }
  },
)

it.each(avatarVariants)('keeps skin and eyes visible through %s glasses', (variant) => {
  const sprite = avatarSprite({ ...fallbackAvatar, variant, accessory: 'glasses' })
  for (const [grid, dx] of [
    [portraitPixels(sprite), 0],
    [spritePixels(sprite), 1],
  ] as const) {
    for (const x of [4, 9]) expect(grid[6]![x + dx]).toMatch(/^[fs]$/)
    for (const x of [5, 8]) expect(grid[6]![x + dx]).toMatch(/^[eo]$/)
    for (const x of [3, 6, 7, 10]) expect(grid[6]![x + dx]).toBe('o')
  }
})

it('maps every palette field to the supplied avatar and keeps the background in both forms', () => {
  const spec = {
    ...fallbackAvatar,
    variant: 'long-hair' as const,
    accessory: 'scarf' as const,
    hair: '#8754B3',
    background: '#EFE9F9',
  }
  const view = render(<AgentAvatar spec={spec} size={20} label="Ada" />)
  const avatar = screen.getByRole('img', { name: 'Ada' })
  expect(avatar).toHaveStyle({
    width: '20px',
    height: '20px',
    backgroundColor: spec.background,
    imageRendering: 'pixelated',
  })
  for (const [variable, value] of Object.entries({
    '--sprite-tone': spec.outfit,
    '--pixel-face': spec.tone,
    '--pixel-skin': spec.tone,
    '--sprite-hair': spec.hair,
    '--sprite-highlight': spec.highlight,
    '--pixel-gold': spec.accent,
  }))
    expect(avatar.style.getPropertyValue(variable)).toBe(value)
  view.rerender(<AgentAvatar spec={spec} size={96} form="sprite" label="Ada" />)
  expect(screen.getByRole('img', { name: 'Ada' })).toHaveStyle({
    height: '96px',
    backgroundColor: spec.background,
  })
  expect(screen.getByRole('img', { name: 'Ada' }).querySelector('svg')).toHaveAttribute(
    'viewBox',
    '0 0 16 28',
  )
})

it('renders a safe avatar when stored data is malformed', () => {
  const view = render(
    <AgentAvatar spec={{ ...fallbackAvatar, variant: 'robot' } as never} label="Fallback" />,
  )
  const before = view.container.innerHTML
  view.rerender(<AgentAvatar spec={{ ...fallbackAvatar }} label="Fallback" />)
  expect(view.container.innerHTML).toBe(before)
})
