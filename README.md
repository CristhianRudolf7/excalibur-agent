# Excalibur

A personal AI agent for conversations, research, everyday work and programming, with a Go backend and a React web app.

[Website](https://excalibur.crehlabs.com) · [Documentation](https://excalibur.crehlabs.com/docs/) · [Source](https://github.com/CristhianRudolf7/excalibur-agent)

## Install

Linux and macOS:

```sh
curl -fsSL https://excalibur.crehlabs.com/install.sh | sh
```

Windows PowerShell:

```powershell
irm https://excalibur.crehlabs.com/install.ps1 | iex
```

Docker:

```sh
docker run -d --name excalibur -p 127.0.0.1:7787:7787 -v excalibur-data:/data ghcr.io/cristhianrudolf7/excalibur:latest
```

The release installation includes the backend and web app together. Open
[http://127.0.0.1:7787](http://127.0.0.1:7787). Docker needs a running Docker
engine; optional browser and robotics dependencies depend on the image and host.

Update with:

```sh
excalibur update
```

To uninstall a native installation, stop it with `excalibur daemon stop`, remove
its autostart entry if enabled, and remove the installed executable from your
PATH. The default autostart entry is the user `excalibur.service` on Linux,
`com.excalibur.daemon` LaunchAgent on macOS, or `Excalibur Daemon` Task Scheduler task
on Windows. Keep your data directories unless you intend to erase conversations,
files and credentials. See the [platform guides](#supported-platforms) for paths.
There is no `excalibur uninstall` command in the current CLI.

For Docker, `docker rm -f excalibur` removes the container and retains the
`excalibur-data` volume. Removing that volume separately erases its stored data.

## Quick start

1. Open the web app and go to **Settings → Connections** to connect a Codex
   account. Alternatively, configure an API provider in the daemon's environment
   or layered configuration and restart the daemon. For example, set
   `EXCALIBUR_PROVIDER=openai`, `EXCALIBUR_MODEL` to a model available to your
   account, and `OPENAI_API_KEY` through your service environment. Keep credentials
   out of chat and version control. See [model configuration](docs/development/wire-engine.md).
2. Open Excalibur's conversation and send a first request, such as “Help me plan
   my week.” A model connection is required for real answers; an unconfigured
   non-interactive run fails explicitly.
3. Enable optional connections and dependencies as needed. Google requires OAuth
   client configuration before connecting; browser tools need Chrome and the
   Playwright driver. Microphone and camera access require browser permission
   and a secure context, such as localhost or HTTPS.

## Features

### Conversation, agents and browsing

- **Conversation:** streaming chat, attachments, Markdown and math, history, reconnect/replay, cross-conversation recall, and English/Spanish UI with light/dark themes.
- **Voice and calls:** dictation, spoken replies and live voice calls use daemon audio services with browser speech fallbacks; server audio requires separate credentials and browser support varies.
- **Agents and subagents:** create personal agents with instructions, model settings and pixel avatars; delegate work with live progress, steering, pause, resume and cancellation.
- **Live browser:** the agent can browse with Playwright while you watch its live view; this is a watch-only panel, with no manual takeover.
- **Vault:** encrypted credentials, domain-bound browser references and secure login forms keep submitted passwords separate from conversation text.
- **Web research:** configurable Tavily/Brave search, page reading and source tracking; search requires provider credentials and applicable storage rights.

### Knowledge, data and documents

- **Memory:** scoped retrieval, consolidation, contradiction review and optional embeddings retain useful context; embedding outages degrade retrieval rather than invent results.
- **Library and citations:** ingest and retrieve documents, search their contents, and open source references with page or paragraph citations.
- **Google tools:** connect Gmail, Drive and Google Docs for mail, files and document edits; sensitive sends, sharing and destructive actions require explicit approval.
- **My Data:** personal typed collections offer relations, filters, sorting, summaries, trash/restore and CSV/XLSX import/export.
- **Files and viewer:** explore registered project files, inspect recorded changes, edit text/code and view PDFs, images and office documents with format-specific previews.
- **Spreadsheets:** open tabular files in the spreadsheet panel; this is a lightweight view/editor, with a limited formula evaluator rather than a complete Excel implementation.
- **Whiteboard:** editable drawings, diagrams, charts and interactive teaching boards remain connected to the conversation.
- **Artifacts and office tools:** create visual artifacts, charts, diagrams, presentations and documents, with export/conversion dependent on the format and installed renderers.

### Work and programming

- **Automations:** persisted schedules and versioned steps support runs, logs and retries; the core agenda also supports triggers/workflow simulation, while classic web automations have no event triggers or webhooks.
- **Reminders:** create, reschedule and dismiss reminders with timezone-aware delivery and live updates.
- **Tasks and projects:** inspect requirements, plans, criteria and evidence, follow delegated runs, and register project roots with isolated scopes.
- **Coding view:** watch commands, test summaries, subagent activity and per-edit diffs inside the originating conversation, with expandable results and a final changes card.
- **Public previews:** publish a built static app or an existing local HTTP app through a temporary Cloudflare link after an explicit request; `cloudflared` is required.
- **Programming tools:** terminal sessions, file search/editing, snapshots, edit history, LSP/code graph integration, leases and worktrees support verifiable coding work.

### Learning, robotics and extensions

- **Classroom:** build personalized courses with editable syllabuses, chat/whiteboard classes, server-graded quizzes and a final exam.
- **Robotics Lab:** run simulated robot sessions with physics, keyframe actions, environment generation, attempts/replays and Markdown session skills; Python dependencies require an explicit installation.
- **Robot arm:** the UR10e integration provides a local Python bridge, state/camera views, named poses and approved movements; physical setup and on-site safety review are required, and simulator tests do not certify hardware safety.
- **Extensions and store:** the CLI validates and installs local extension packages; the web catalog toggles bundled Classroom and robotics views locally. A production remote marketplace is not implemented.
- **Skills:** browse, load, edit and enable skills, with external installation validation and drift detection; automatic skill learning is out of scope.
- **Other capability contracts (partial):** GitHub, device/media, remote-node and GPU-job contracts include deterministic stand-ins; their presence does not establish live external service support.
- **Messaging channels (partial):** channel interfaces and confirmation contracts exist, but bundled messaging/meeting backends are simulations; there is no concrete Telegram or WhatsApp transport in this repository.

### Models and command line

- **Providers and accounts:** OpenAI, Anthropic, Gemini, DeepSeek, OpenRouter, Ollama and generic OpenAI-compatible adapters support model routing and usage accounting; the daemon also supports a connected Codex account.
- **Settings:** manage profile, timezone, personal agents, connections, passwords, skills, Robotics Lab and recorded usage/costs; provider capabilities and account allowances vary.
- **CLI:** `chat`, `exec`, `status`, `tasks`, `projects`, `memory`, `skills`, `task`, `plan`, `vault`, `ext`, `daemon`, `install`, `update`, `eval` and `bench` expose conversation, background work, administration and evaluation; run `excalibur help` for the command tree.
- **MCP and ACP:** expose delegated work over stdio to compatible clients, including MCP client-fragment installation with `excalibur mcp install`.

## Terminal-Bench 2.1 results

The measured Excalibur version is tag **`tb21-59.6`**. All harnesses used the same
89 tasks and OpenAI `gpt-6-luna` at reasoning effort `low`, one attempt per task,
through Harbor 0.25 (November 2026).

| Harness | Score | Tasks passed | Mean minutes per task | Input tokens | Cache hit | Output tokens | Cost, full run |
|---|---:|---:|---:|---:|---:|---:|---:|
| **Excalibur** | **59.6%** | **53/89** | 11.3 | 126 M | 91% | 1.60 M | **$1.55** |
| OpenCode | 55.1% | 49/89 | 10.4 | 24 M | 100% | 0.18 M | $0.17 |
| Excalibur (previous version) | 52.8% | 47/89 | 12.5 | 116 M | 73% | 1.55 M | $2.36 |
| Terminus 2 | 51.7% | 46/89 | 12.8 | 170 M | 99% | 1.16 M | $1.23 |
| Codex | 48.3% | 43/89 | 9.7 | 103 M | 98% | 0.44 M | $0.73 |

Excalibur's run used OpenAI's standard tier; the other harnesses ran on Flex.
All costs in the table are computed from token usage at Flex prices.
At standard prices, Excalibur's run costs **$3.09**. These results describe the
measured tag, not a new measurement of the current release. See
[benchmark tooling and verification](docs/development/terminal-bench.md).

## Supported platforms

| Platform | Support | Feature-specific limits |
|---|---|---|
| Linux | Supported | User autostart uses systemd; Secret Service requires a desktop session or a configured passphrase fallback. |
| Windows | Supported | Interactive terminals require Windows 10 1809+ ConPTY; Linux cgroup/network namespace isolation is unavailable. |
| macOS | Supported on Apple Silicon and Intel | Robotics Lab requires Apple Silicon; LaunchAgent autostart requires a GUI login session; Linux cgroup limits are unavailable. |

Native service, keyring, browser and robot rendering checks require the target
OS and its dependencies. Cross-compilation alone does not verify those features.
See [platform backends](docs/development/platform.md),
[Windows](docs/development/windows.md) and [macOS](docs/development/macos.md).

| Feature | Additional prerequisites |
|---|---|
| Browser tools/live view | System Chrome and Playwright driver (`excalibur install` installs the driver). |
| Robotics Lab | Python 3.10+ and optional physics/ML dependencies installed through the Lab settings; platform rendering libraries vary. |
| UR robot arm | Reachable compatible controller, Python bridge dependencies and reviewed physical installation; URSim checks need Docker. |
| Public previews | `cloudflared` on the daemon PATH, or `EXCALIBUR_CLOUDFLARED`. |
| Google | OAuth client configuration, consented scopes and a connected account. |
| Voice/camera | Browser permission and localhost/HTTPS; server voice requires audio API credentials. |
| Some document conversions | Format-specific tools, including LibreOffice for legacy conversion. |

## Build from source

Use Go **1.27.3 or newer** (see `go.mod`). The web workspace requires Node.js
**22+** and pnpm **12.16.1**. A C compiler is required for the native SQLite build.

```sh
git clone https://github.com/CristhianRudolf7/excalibur-agent.git
cd excalibur-agent
go build -o bin/excalibur ./cmd/excalibur
go build ./...
```

The default build uses portable SQLite. To enable native SQLite FTS5 and vector
support on a compatible cgo toolchain:

```sh
CGO_ENABLED=1 go build -tags sqlite_fts5 -o bin/excalibur ./cmd/excalibur
```

Install web dependencies from the repository root, then work in the web app:

```sh
pnpm install --frozen-lockfile
cd apps/web
pnpm dev
```

`pnpm dev` starts Vite at `http://localhost:5173` and proxies API requests to a
separately started daemon (`excalibur daemon start --listen 127.0.0.1:7787`).
Alternatively, from `apps/web`, run:

```sh
pnpm dev:full
```

This starts Vite and builds/starts a daemon with disposable temporary data.
It inherits your environment, so configure a provider before launching it.
For a production web build, run `pnpm build` in `apps/web`.

Backend verification is `make verify` (formatting, vet, staticcheck, goroutine
guard, race tests and cross builds). Web verification is `pnpm check` and
`pnpm build` in `apps/web`; browser flows use `pnpm test:e2e` and system Chrome.
See the [web developer guide](apps/web/README.md).

## Architecture and repository layout

One daemon owns durable state and coordinates conversations, tools and delegated
runs. The web client uses JSON-RPC 2.0 over HTTP, WebSocket events and blob
endpoints. SQLite stores, a journal, scoped memory and verification gates support
recovery and traceable work. Providers run through the model gateway. The eval
profile has a frozen tool/prompt surface separate from assistant-only features.

| Path | Contents |
|---|---|
| `cmd/excalibur/` | Go binary and command dispatch. |
| `internal/` | Engine, providers, daemon, tools, storage, platform backends and assistant services. |
| `apps/web/`, `apps/site/` | React web app and Astro public site/documentation. |
| `apps/cli/` | Reserved CLI packaging directory; the command itself lives in Go. |
| `packages/contracts/`, `packages/ui/` | Wire schemas and shared UI directory. |
| `pkg/extsdk/`, `extensions/` | Public extension SDK, examples and UR robot integration. |
| `store/` | Reserved catalog/examples/installer directories; no published remote catalog. |
| `integrations/`, `scripts/bench/` | Client/Harbor adapters and benchmark launchers. |
| `docs/`, `tests/` | Implementation guides, historical decisions and integration/verification suites. |

Start with the [architecture overview](docs/architecture/overview.md),
[development evidence](docs/development/coverage.md) or
[extension guide](docs/development/extending.md). The original architecture
specification is external; repository docs reference its section numbers.

## Security

The daemon binds to localhost by default. Tools can execute commands and modify
files with your user's permissions; the platform isolator is not a general
sandbox. Keep the daemon private and review extensions before enabling them.
Do not place credentials in prompts, source files or public artifacts.

The vault encrypts stored secrets and applies reference policies and redaction.
Some connections can use protected local credential files when the vault is
unavailable; this is not a promise that every connection token is always encrypted
at rest. See [vault](docs/development/vault.md) and
[secure credential requests](docs/development/secure-credentials.md).

**Public previews expose an app to anyone with the link.** Publish only content
intended for public access, stop previews when finished, and keep private data
and credentials out of the served app. See [preview lifecycle](docs/development/public-preview.md).
A preview link provides no access control. Robot software checks and a camera
do not replace physical safety systems or supervision.

## License and third-party notices

Excalibur uses the [Excalibur Source-Available License](LICENSE). It permits
personal, research, non-commercial and internal organizational use. Commercial
distribution and commercial services offered to third parties require separate
written permission. The license text governs.

Dependencies retain their own licenses and notices. See
[third-party notices](THIRD_PARTY_NOTICES.md),
[dependency records](docs/decisions/dependencies.md) and bundled asset attributions.
