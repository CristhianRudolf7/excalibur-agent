# Changelog

Notable changes are listed by release. Test evidence and feature limits live in
`docs/development/`; historical decisions may describe superseded states.

## [Unreleased]

No additional changes recorded.

## [0.1.0]

### Added

- Personal AI assistant with a Go daemon, React web app and Astro public site.
- Streaming conversations, personal agents and avatars, delegated subagents,
  steering, scoped memory, document library and source citations.
- Live browser viewing, secure vault-backed login requests, web search,
  dictation, speech output and live voice calls with fallbacks.
- Google OAuth connection and Gmail, Drive and Google Docs tools.
- My Data collections, CSV/XLSX import/export, file and office-document views,
  spreadsheet editing, interactive whiteboards and visual artifacts.
- Project/task inspection, verifiable plans and evidence, reminders, durable
  scheduled automations and workflow simulation.
- Live coding activity with commands, observed test summaries and per-edit diffs;
  temporary public previews of static or local HTTP apps by explicit request.
- Classroom courses, server-graded quizzes and final exams; Robotics Lab
  simulation and replay; UR10e bridge and local URSim verification tooling.
- Model adapters for OpenAI, Anthropic, Gemini, DeepSeek, OpenRouter, Ollama and
  generic OpenAI-compatible endpoints, plus daemon Codex account connection.
- CLI task administration, local extension validation/installation, skills,
  MCP/ACP adapters, evaluation tooling and Terminal-Bench launch/report commands.
- Native Linux, Windows and macOS backends, durable SQLite stores, journal-based
  recovery and encrypted secret storage.

### Changed

- Replaced the license draft with the owner's Excalibur Source-Available License.
- Rewrote installation, feature and platform documentation for the public release,
  including partial messaging/store support and native verification limits.
- Identified `tb21-59.6` as the measured benchmark version and clarified standard
  execution versus Flex-normalized costs, without changing the recorded results.

### Removed

- The unfinished 2D world interface, its navigation, shortcut, tests and exclusive
  assets. Earlier implementation remains in git history; development continues.
- Repository-local OpenCode configuration and unused committed loading-state
  screenshots. Generalized local paths and translated the robot setup guide.
