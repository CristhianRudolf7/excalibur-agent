import { afterEach, expect, it } from 'vitest'
import { fallbackAvatar, paladinAvatar, type AvatarSpec } from '@/components/avatar-spec'
import { agentAvatar } from './avatar'
import { defaultAgent, useAgents } from './store'

// Captured defaults from the former web fallback and the daemon seed.
const legacyAvatars: AvatarSpec[] = [
  {
    variant: 'hood',
    tone: '#C99871',
    hair: '#302A38',
    highlight: '#9D8DDF',
    outfit: '#504378',
    accent: '#E6C56B',
    accessory: 'none',
    background: '#262135',
  },
  {
    variant: 'beard',
    tone: '#DCA879',
    hair: '#84613F',
    highlight: '#F7D5E9',
    outfit: '#9A6481',
    accent: '#B9D788',
    accessory: 'none',
    background: '#F5E8EE',
  },
]

const initial = useAgents.getState()
afterEach(() => useAgents.setState(initial, true))

it('seeds Excalibur with the Paladin preset', () => {
  expect(defaultAgent.avatar).toEqual(paladinAvatar)
})

it.each(legacyAvatars)(
  'replaces only a built-in legacy default at ingestion: $variant',
  (avatar) => {
    const builtIn = { ...defaultAgent, id: 'agt_server_id', avatar, favorite: false }
    const other = { ...builtIn, id: 'other', conversation_id: 'cnv_agent_other', favorite: true }
    const list = { agents: [builtIn, other], favorite_id: other.id }
    useAgents.getState().apply(list)
    expect(useAgents.getState().agents[0]!.avatar).toEqual(paladinAvatar)
    expect(useAgents.getState().agents[1]!.avatar).toEqual(avatar)
    expect(list.agents[0]!.avatar).toEqual(avatar)
    expect(list.agents[0]!.favorite).toBe(false)
  },
)

it('recognizes the old web identity and lowercase stored palette', () => {
  const avatar = Object.fromEntries(
    Object.entries(legacyAvatars[0]!).map(([key, value]) => [key, value.toLowerCase()]),
  ) as unknown as AvatarSpec
  expect(agentAvatar({ id: 'excalibur', conversation_id: 'other', avatar })).toEqual(paladinAvatar)
})

it('preserves custom built-in avatars and their refreshes after a Settings avatar replacement', () => {
  const builtIn = { ...defaultAgent, avatar: legacyAvatars[1]! }
  useAgents.getState().apply({ agents: [builtIn], favorite_id: builtIn.id })
  const custom = { ...builtIn, avatar: { ...fallbackAvatar, variant: 'headphones' as const } }
  for (let refresh = 0; refresh < 2; refresh++) {
    useAgents.getState().apply({ agents: [custom], favorite_id: custom.id })
    expect(useAgents.getState().agents[0]!.avatar).toEqual(custom.avatar)
  }
})

it('does not treat a modified legacy palette or accessory as a default', () => {
  for (const avatar of [
    { ...legacyAvatars[1]!, outfit: '#123456' },
    { ...legacyAvatars[1]!, accessory: 'glasses' as const },
  ])
    expect(agentAvatar({ ...defaultAgent, avatar })).toEqual(avatar)
})

it('keeps the Paladin preset on other agents that explicitly use it', () => {
  expect(
    agentAvatar({ id: 'paladin', conversation_id: 'cnv_other', avatar: paladinAvatar }),
  ).toEqual(paladinAvatar)
})
