import { paladinAvatar, type AvatarSpec } from '@/components/avatar-spec'
import type { Agent } from './types'

// The old web fallback and the daemon's DefaultAvatar("Excalibur") seed.
// Settings requests persist a replacement spec through agents.update, without
// a separate choice flag. Match only these defaults so custom palettes survive.
const legacyDefaults: readonly AvatarSpec[] = [
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

/** Resolve the built-in identity once, for every avatar consumer of the store. */
export function agentAvatar(agent: Pick<Agent, 'id' | 'conversation_id' | 'avatar'>): AvatarSpec {
  const builtIn = agent.id === 'excalibur' || agent.conversation_id === 'cnv_web_excalibur'
  const spec = agent.avatar
  if (
    builtIn &&
    spec &&
    legacyDefaults.some((seed) =>
      (Object.keys(seed) as (keyof AvatarSpec)[]).every(
        (key) =>
          typeof spec[key] === 'string' && spec[key].toUpperCase() === seed[key]?.toUpperCase(),
      ),
    )
  )
    return paladinAvatar
  return spec
}
