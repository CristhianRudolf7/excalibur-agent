# Real browser wiring

BR-3 connects Section 14.2 to Playwright 1.62.1 through the pinned MIT
`github.com/mxschmitt/playwright-go v0.6201.1` binding, using system Chrome.
There is one persistent profile under the daemon data directory, shared by
logical sessions. Chrome installation remains the operator's responsibility;
`excalibur install` installs only the driver and reports browser readiness as
a nonfatal step. The installer accepts an injected browser step for offline
unit tests. There is no separate doctor command in the current CLI.

Production daemon startup and embedded default/delegate exec opt into the real
collaborators. Unavailable or disabled browsers retain the deterministic fake
with a `browser disabled: ...` warning. `DefaultRegistry` remains unchanged.
Embedded eval never creates these collaborators, probes availability, resolves
search keys or adds browser tools. The nil shared catalog options retain the
previous names, prefix and profile fingerprints. `EmbeddedOptions.Web` is the
additive search composition field (the actual repository equivalent of the
brief's `BuildOptions.Web`). No Tool, API or capability manifest changed.

Defaults are headless, sandbox enabled, Chrome channel, a ten-minute idle
suspension timeout and eight tabs. Profile, downloads and routines directories
are private (0700). Sandbox overrides warn. Uploads are limited to registered
project roots at startup and the downloads directory; embedded runs allow their
project directory and downloads. Captures/downloads use the daemon blob store;
embedded captures use private, content-addressed files with atomic publication.
Shutdown closes the engine and flushes its persistent profile.

Browser form secrets resolve with `browser_act`, the page hostname and form
policy at execution time. Delegates retain project-only restrictions. Browser
`input_required` results tell the model that a human is required, there is no
takeover in this version, and it should notify the user and use another source,
`web_read` or search. The adapter removes the legacy headed-mode instruction.
Neither a web UI nor human takeover is implemented.

For JS-heavy public page reads, the same Chrome process creates an ephemeral
context without profile credentials. Service workers are blocked, WebSockets
are closed and each GET resource is fulfilled by the guarded HTTP reader.
Other HTTP methods are refused. DNS validation, direct validated-IP dialing,
redirect validation, benchmark-source rejection and bounded response sizes
remain in force. Tests do not weaken production SSRF rules for localhost.

Future work: live view and takeover, prompt-injection defenses, confirmations
for sensitive actions, Landlock/seccomp confinement and an egress policy.
The renderer request guard is not a general browser process confinement policy.

Verification is recorded in [browser-search.md](../development/browser-search.md).
