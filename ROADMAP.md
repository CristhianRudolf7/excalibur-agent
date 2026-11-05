# Roadmap

## Available in 0.1.0

The programming harness and personal assistant share one Go daemon and a React
web interface. Implemented areas include:

- Durable execution, verifiable tasks/plans/evidence, terminal/file tools,
  context/cache management, provider routing, subagent control and recovery.
- Persistent conversations and personal agents, scoped memory, document library,
  citations, a secrets vault and secure browser credential requests.
- Live browser viewing, web research, dictation and voice calls, with optional
  credentials and browser-dependent fallbacks.
- Google account connection with Gmail, Drive and Docs tools; personal typed
  collections with spreadsheet import/export.
- File/document viewers, spreadsheet editing, whiteboards, visual artifacts,
  coding activity/diffs and explicitly requested public previews.
- Timezone-aware reminders, scheduled automations, project/task inspection,
  Classroom, Robotics Lab and the UR robot bridge.
- Local extensions, skills, MCP/ACP adapters and native Linux, Windows and macOS
  platform backends. Feature prerequisites and limits are in the root README.

The original H0–H13 milestones have implementations and backend verification
coverage. [Coverage](docs/development/coverage.md) records the section-level test
evidence; it distinguishes simulated capability contracts from live features.
The benchmark table describes the historical `tb21-59.6` tag, not a new test of
0.1.0.

## In development and current gaps

2D world: in development. Removed from the current interface until it is ready; earlier work is in the git history.

- A production remote extension/skill marketplace and general remote store
  installer are not implemented. Local installation and validation exist.
- Concrete messaging transports such as Telegram and WhatsApp remain absent;
  channel/meeting simulations exercise the contracts.
- Native service, keyring, browser, camera and robotics checks still depend on
  OS-specific runners and hardware. Cross builds establish compilation only.
- Tool isolation varies by feature and OS; the generic platform isolator is a
  pass-through. Public preview URLs are public access links.
- Mobile packaging, autopoiesis and automatic skill learning are out of scope.

No release dates are promised for the remaining work.
