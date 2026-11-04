# Windows daemon platform support

Sections 3.4, 20.1 and 25.2–25.3 remain behind the existing platform
interfaces. Windows paths use the local and roaming application known folders,
with environment fallback. Runtime directories and secret files receive a
protected DACL granting access only to the current user and SYSTEM. The new
`platform.RestrictToOwner` and `platform.IsOwnerOnly` helpers are additive;
the Paths, Keyring and ServiceManager contracts do not change.

The limited per-user Task Scheduler task uses an interactive logon trigger,
starts immediately at install, ignores duplicate launches and restarts on
failure. XML is passed to schtasks because its basic ONLOGON command cannot
express the restart policy. The scheduler's minimum one-minute restart interval
is used. When schtasks is unavailable, HKCU Run provides logon autostart without
restart supervision. Status reports active, inactive, disabled or installed;
localized output without a recognized state reports installed.

The existing gate has no shutdown RPC or HTTP route. A Windows-only, user-private
named event requests normal daemon context cancellation through additive
`platform.ListenDaemonStop` and `platform.RequestDaemonStop` helpers. Stop waits
five seconds, matching the service-loop grace period, before terminating the
process with a successful exit code so an explicit stop does not trigger the
restart policy. Discovery files are cleared after confirmed exit, unless a
new daemon PID has replaced them. No public API methods are added. Linux and Darwin retain SIGTERM.

Windows image updates rename the executable to .old before moving the candidate
into place. Failure restores the executable; rollback uses the same operation
with a .bad backup. The new `daemon.CleanupPreviousImage` hook removes old image
files on the next process start, best effort if an old process still holds them.
Other platforms retain the previous .prev update behavior.

OS-specific helpers preserve existing Linux, Darwin and other-platform behavior
for configuration, CLI state, extension discovery, automations, principal
defaults, console echo, daemon rendezvous and sd_notify. Windows configuration
uses roaming application data for the user layer and ProgramData for the system
layer. The legacy `config.SystemDefaultPath` constant remains available; actual
layer resolution uses the platform helper. No provider paths, model wiring,
protocol adapters or tool definitions change.

Windows tests cover paths, DACL rejection and acceptance, credential roundtrip,
console restoration when attached to a console, discovery and image swap and
rollback. Portable tests cover Windows argument quoting, task XML and scheduler
status parsing. Windows execution remains to be verified on a Windows runner.
