import { createWorldStore } from './store'
import { campus } from './seed/layout'
import { agents, subagents } from './seed/agents'
import { missions } from './seed/missions'
import { initialDialog, initialHistory } from './seed/timeline'
const now = Date.now()
export const worldSession = createWorldStore({
  layout: campus,
  agents: [...agents, ...subagents.slice(0, 3)],
  missions,
  history: [initialDialog(now), ...initialHistory(now)],
  dialogs: [initialDialog(now)],
})
