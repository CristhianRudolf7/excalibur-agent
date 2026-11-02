# Capabilities, automation seams and extensions

Contract and backend tests of the w3-capabilities wave (milestone
H13): the general capability contract and every adapter with contract
fakes (14.1-14.11), the MCP client over the official Go SDK, the
extensions kit of 14.12 (four routes, `pkg/extsdk`,
`excalibur-extension.yaml`, hot add, `excalibur ext` CLI,
conformance suite, one example per route), and the remaining
personal-assistant deferred tools of 13.3 (`email`, `calendar`,
`meeting`, `contact_message`, `github`, `userdb`, `gpu_training`,
`nodes`, plus the `settings` and `reminders` core providers).

Architecture reference: Sections 13.3, 14.1-14.13. Appendix B values
used verbatim (`DefaultPollInterval` 2 s). See
`docs/decisions/0023-capabilities-h13.md` for the wave decisions.

## Contract

- **General contract** (`internal/capabilities/capability.go`,
  `schema.go`): `Manifest` (identity, version, operations with
  schemas, effect/idempotency classes, platforms/profiles, resources,
  concurrency, reportable states), `Operation` with the full Section
  13.1 execution fields (`TimeoutSecs`, `OutputBudget`, `Inverse`,
  `Probe`, `DryRun`) plus `Group`/`Tags`/`Keywords` for `search_tools`
  discovery, `TaskState`
  (`working`/`input_required`/`completed`/`failed`/`cancelled`) with
  `Terminal`, `ErrorClass` with `ClassifyError` for the Section 17.7
  retry policy, `TaskRecord` with idempotency key and poll interval for
  resume after restart, `KeyDedup` (repeats with the same key never
  repeat effects), `ObjSchema`/`AnySchema`/`Op` builders. The harness
  converts each operation into a deferred tool through the standard
  pipeline (`internal/tools/capability_adapter.go`, honoring the
  operation timeout/budget/group/tags); polling follows `working` to a
  terminal state, `input_required` surfaces its need for the engine
  `ask_user`, `failed` maps by error class, `cancelled` maps to
  canceled, and repeats carry the same idempotency key.
- **Browser** (`browser.go`, 14.2): 13 operations, cooperative
  navigation/actions, immediate reads. The fake enforces the harness
  rules: no act/capture without `browser_wait_ready`, refs bound to
  their snapshot (`stale_ref` otherwise), blocked states
  (`blocked_captcha`, `needs_login`, `needs_2fa`, `blocked_by_site`)
  via `input_required` resumable with `Update`, deterministic routine
  replay with per-step assertions (a failed assertion returns control
  to the agent at that step), max 8 sessions.
- **Vision** (`senses.go`, 14.4): `vision_extract` with
  `text`/`layout`/`describe` modes, per-page provenance and the
  reported model.
- **Voice** (`senses.go`, 14.5): `audio_transcribe` for files and
  `meeting_transcribe` with a consent gate (no consent, no
  transcript). `stt_stream`/`tts_stream` stay on the voice channel.
- **Device** (`senses.go`, 14.6): screen capture, accessibility tree,
  input, app windows. Each node declares its operations; unsupported
  ones fail explicitly instead of hanging.
- **Documents and media** (`documents.go`, 14.7): `document_read`
  with page/section/paragraph anchors, create/edit with version
  history, `pdf_ops`, `file_convert` on the manifest allowlist only
  (`md->html`, `txt->md`, `md->txt`, `json->md`, `csv->json`,
  `html->md`), `library_ingest`, `userdb` (create/schema/migrate with
  versioning, query, import/export) apart from the internal stores.
  `diagram_render` renders five dialects to SVG with concrete syntax
  errors (raster formats are an honest input error),
  `chart_render` for bar/line, `image_generate`/`image_edit` with
  reported cost.
- **Apps** (`apps.go`, 14.8): `app_state` (compact state, recent user
  actions, scope `app:<id>`, sequence number) plus one operation per
  application. Manual user changes append events readable as the L3
  compiler delta (`DeltaSince`); the classroom completion rule
  (a topic completes only with every answer correct) is enforced as a
  check the harness treats as an acceptance criterion.
- **Messaging, workspace, meetings** (`channels.go`, 14.9): a channel
  is only a door. `contact_message` and mail sending need explicit
  user confirmation and are single-effect under the same key;
  `calendar` reads are fast and writes declare their inverse;
  `meeting` requires joining as an assistant (never as the user) and
  recording consent before transcribing.
- **Nodes** (`nodes.go`, 14.10): pairing with declared capabilities,
  `nodes` listing, `ParseTarget` for `node:<id>`, `Exec` on connected
  nodes through the same pipeline shape, declared error or `working`
  wait on disconnected ones, re-poll on reconnect.
- **Services** (`services.go`): `gpu_training` jobs with metrics,
  store install/publish requests gated by the conformance suite,
  `github` issues/PRs/comments/commits/compare/releases/CI, and the
  deferred Section 14.11 user representation as an explicit permanent
  refusal (needs the Section 20 disclosure filter and representation
  policy).
- **MCP client** (`mcp.go`): `DialMCP` for stdio commands (relative to
  the extension directory), streamable HTTP URLs and (rejected, for
  now) Unix sockets over the official Go SDK
  (`github.com/modelcontextprotocol/go-sdk`). `ExcaliburMeta` reads
  `_meta` under `excalibur/` (bare `excalibur` accepted) and derives
  missing fields from the MCP annotations with the most conservative
  mapping (readOnlyHint to read_only/pure/immediate, idempotentHint to
  write_local/idempotent, otherwise
  write_external/non_idempotent/cooperative). `ManifestFromMCPTools`
  converts to the capability manifest with deterministic order and
  Section 13.3 catalog names; `TaskResultFromMCP` maps content and task
  handles to the durable-task states.
- **Personal deferred tools** (`internal/tools/personal.go`, 13.3):
  `email`, `calendar`, `contact_message`, `meeting`, `github`,
  `userdb`, `gpu_training`, `nodes`, each against its capability
  contract with a memory fake, registered deferred through the same
  registry (`RegisterPersonalDeferred`, first registration wins so an
  owner wave can replace a fake). `WirePersonalCore` fills the
  `settings` and `reminders` core stub providers only when unset, so
  the owning waves (agenda, config) win whenever they wire.
- **Extensions kit** (`internal/tools/extensions*.go`, 14.12):
  `Loader` with discovery (`LoadDir`), validation, activation as
  deferred tools with in-flight tracking (`Update` waits for draining
  calls, unsupported contracts rejected), `Enable`/`Disable`/`Remove`,
  `Doctor`, fsnotify `Watch` (one subdirectory level, retries on
  partial writes) and `Close`. Route 1 dials MCP servers lazily and
  lists tools once at bind; route 2 invokes a bound `AppChannel`
  (`MemoryAppChannel` over the apps fake with compiler-delta events);
  route 3 delegates to the `PackageManager` (tests through the 13.4
  pipeline, dry-run for effectful tools); route 4 pairs manifests with
  in-process tools via `ProvideBuiltin`. `MapRegistry.Unregister` and
  `DeferredCatalog.UnmarkDeferred` exist for extension retirement.
  `RunConformance` is the `excalibur ext test` suite: manifest and
  schema validity, positive timeouts/budgets, `dry_run` on effectful
  tools, unique names, same-key repeat probes (same outcome class,
  outputs within budget), cancel honored within 3 s on a canceled
  context. `ActivateChecked` is the store gate: sandbox activation and
  suite first, live activation only on pass; failures never register.
  `Loader.PrefixHash` exposes the fixed-prefix hash for the
  never-changes assertion.
- **SDK** (`pkg/extsdk`): `Manifest.Validate`/`Definition.Validate`
  mirroring the internal rules, `ToolMeta` with `_meta` map helpers,
  `MetaKey` constants, and YAML parsing/rendering
  (`UnmarshalManifestYAML`/`MarshalManifestYAML`: duration strings or
  nanosecond timeouts, schemas as YAML maps or inline JSON). The
  internal loader converts through the JSON view the parity test pins.
- **Examples** (`extensions/example-*`): one per route (MCP stdio
  server with `excalibur/` meta, app operation, tool package with
  passing tests, in-process builtin with fixtures), each with
  `excalibur-extension.yaml`, `probes.json` and a README.
- **CLI** (`internal/cli/ext.go`, `cmd/excalibur/main.go`): `ext new`
  scaffolding, `list`, `install` (local dirs; store refs need the
  store capability), `enable`/`disable` markers, `remove`, `logs`,
  `test` (sandbox gate plus prefix-stability check) and `doctor`
  (validation plus SDK parity). The `cmd` wiring only replaces the
  `ext` subcommands; the `main_test.go` not-implemented case for
  `ext doctor` moved to a positive H13 test.

## Backend tests

- `internal/capabilities`: all 15 manifests validate; browser
  wait-before-act rejection, stale refs, CAPTCHA
  working/input_required/update/resume by persisted task id, routine
  assertion control; same-key resends record one effect; full durable
  progression; voice consent gate; device unsupported-op errors;
  document pairs/version/anchors, userdb unknown-table errors; media
  dialect errors, SVG-only honesty, image cost; apps delta sync and
  classroom gate; confirmation gates; meeting disclosure/consent;
  nodes connected/disconnected/wait/reconnect/capability checks; GPU
  launch/metrics/pause; store single-install; userrep permanent
  refusal; MCP meta precedence/conservative derivation, naming and
  deterministic order, task-handle mapping.
- `internal/tools/personal_test.go`: eight tools registered deferred
  with working providers, discoverable via `search_tools`; every
  confirmation gate; calendar inverse; resend single effects; settings
  get/set/list; reminders create/snooze/complete/list.
- `internal/tools/extensions_h13_test.go`: row 1 (four routes appear
  in `search_tools` without restart/recompile, prefix hash unchanged,
  all invoked through the same pipeline, doctor clean); row 2 (good
  passes, repeating/ignoring-cancellation fails with the exact breach
  and never activates); row 3 (manual change event, delta, next op
  from updated state); row 4 (task-id persistence round-trip,
  re-poll resume, single effect); row 5 (update pending on in-flight
  calls, activation after drain, unsupported contract rejected); row 6
  (blocked op surfaces `input_required` with the block through the
  pipeline); row 9 (allowed reads recorded, benchmark reads refused
  and logged, answer backed only by recorded sources); row 13
  (document round-trip preserves content, format and anchors);
  doctor emptiness; watcher hot add with stable prefix.
- `internal/cli/ext_test.go`: usage, scaffolding per kind, empty
  list/doctor, package `ext test` PASS, doctor listing, missing-dir
  failure, install/list/disable/enable/logs/remove cycle.
- `pkg/extsdk`: validation, MCP meta exception, YAML round-trip and
  shape tolerance (maps vs inline JSON, duration strings vs nanos).
- `cmd/excalibur`: `ext doctor` now exits 0 on an empty root,
  `ext test` on a missing dir exits 1, unknown ext subcommands exit 1.

## Deferred (other H13/H-wave owners)

Route-1 Unix-socket transport, live search/API/voice/model backends
behind the fakes, daemon-gate `capability.*` wiring, store purchase
flows, Section 14.11 enablement with the Section 20 policy, agenda
`schedule`/`trigger_rules`/`workflow`/`flow_simulate` (agenda wave),
code/workspace/device `exec target` wiring (exec owner), and the
`web-to-slides-export` style skill bindings (skills wave). The seams
above (capability interfaces, `ToolsFromCapability`, loader, channel,
`WirePersonalCore` fill-if-unset) are the integration points.

## Production browser and web search

BR-3 connects the real persistent browser and Tavily backend in the daemon and
embedded default/delegate exec. The capability manifest and default fake
registry are unchanged; unavailable/disabled Chrome keeps the fake with a
warning. Eval retains its prior deterministic catalog and search backend.
Browser blocks explicitly tell the model there is no takeover and to notify
the user and use alternatives. See [browser-search.md](browser-search.md) for
configuration, local Chrome tests, shutdown, secret policy and the durable
engine's Brave retention limitation. Decisions 0084 and 0085 record the seams.
