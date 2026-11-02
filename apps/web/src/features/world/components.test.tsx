import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import '@/lib/i18n'
import { ActivityFeed } from './Activity'
import { MissionBoard } from './Missions'
import { Minimap } from './Minimap'
import { PixelSprite, PixelPortrait } from '@/components/PixelSprite'
import { spritePixels } from './sprites'
import { ModeSwitch } from '@/features/shell/ModeSwitch'
import { agents } from './seed/agents'
import { initialHistory } from './seed/timeline'
import { missions } from './seed/missions'
const identities = Object.fromEntries(agents.map((agent) => [agent.id, agent]))
afterEach(cleanup)
it('mode selection emits the chosen mode and exposes the active segment', () => {
  const change = vi.fn()
  render(<ModeSwitch mode="chat" onChange={change} />)
  expect(screen.getByRole('button', { name: 'Chat' })).toHaveAttribute('aria-pressed', 'true')
  fireEvent.click(screen.getByRole('button', { name: 'Mundo' }))
  expect(change).toHaveBeenCalledWith('world')
})
it('mission board accepts different missions, runtime identities and collapse control', () => {
  const toggle = vi.fn(),
    view = render(
      <MissionBoard
        missions={[missions[0]!]}
        agents={identities}
        collapsed={false}
        onToggle={toggle}
      />,
    )
  expect(screen.getByRole('progressbar')).toHaveAttribute('value', '65')
  fireEvent.click(screen.getByRole('button'))
  expect(toggle).toHaveBeenCalledOnce()
  view.rerender(<MissionBoard missions={[]} agents={{}} collapsed onToggle={toggle} />)
  expect(screen.queryByRole('progressbar')).toBeNull()
})
it('activity feed can be embedded without a world store and reports its supplied unread count', () => {
  const toggle = vi.fn(),
    select = vi.fn(),
    view = render(
      <ActivityFeed
        entries={initialHistory(1000000)}
        agents={identities}
        roomLabels={{}}
        collapsed
        unread={7}
        onToggle={toggle}
        onSelect={select}
      />,
    )
  expect(screen.getByRole('button').textContent).toContain('7')
  fireEvent.click(screen.getByRole('button'))
  expect(toggle).toHaveBeenCalledOnce()
  view.rerender(
    <ActivityFeed
      entries={[]}
      agents={{}}
      roomLabels={{}}
      roomId="other"
      collapsed={false}
      unread={0}
      onToggle={toggle}
      onSelect={select}
    />,
  )
  fireEvent.click(screen.getByRole('button', { name: 'Todas las salas' }))
  expect(screen.getByRole('button', { name: 'Todas las salas' })).toHaveAttribute(
    'aria-pressed',
    'true',
  )
})
it('minimap accepts arbitrary shapes, markers and viewport, and emits navigation coordinates', () => {
  const navigate = vi.fn()
  render(
    <Minimap
      width={1000}
      height={700}
      shapes={[
        {
          id: 'new-shape',
          points: [
            { x: 1, y: 2 },
            { x: 50, y: 60 },
          ],
        },
      ]}
      markers={[{ id: 'someone', position: { x: 50, y: 70 }, color: 'var(--pink)' }]}
      viewport={{ x: 100, y: 100, width: 200, height: 150 }}
      onNavigate={navigate}
    />,
  )
  fireEvent.keyDown(screen.getByRole('application'), { key: 'ArrowRight' })
  expect(navigate).toHaveBeenCalledWith({ x: 380, y: 175 })
  expect(document.querySelector('polygon')).toHaveAttribute('points', '0.22,0.44 11,13.2')
  expect(document.querySelector('circle')).toHaveAttribute('cx', '11')
})
it('sprites and portraits accept arbitrary palettes, distinct variants and smaller helper sizes', () => {
  const sprite = { variant: 'long-hair' as const, tone: 'var(--pink)', hair: 'var(--violet)' }
  const view = render(
    <>
      <PixelSprite sprite={sprite} size={84} label="Custom helper" />
      <PixelPortrait sprite={sprite} size={112} label="Custom portrait" />
    </>,
  )
  expect(screen.getByRole('img', { name: 'Custom helper' })).toHaveStyle({ height: '84px' })
  expect(screen.getByRole('img', { name: 'Custom portrait' }).querySelector('svg')).toHaveAttribute(
    'viewBox',
    '0 0 14 14',
  )
  const variants = ['hood', 'long-hair', 'cap', 'bow', 'beard', 'headphones'] as const
  const signatures = variants.map((variant) =>
    JSON.stringify(spritePixels({ variant, tone: 'var(--primary)' })),
  )
  expect(new Set(signatures).size).toBe(6)
  expect(spritePixels(sprite, 2)).not.toEqual(spritePixels(sprite, 3))
  view.unmount()
})
