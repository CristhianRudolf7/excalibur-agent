# macOS platform backends

The macOS implementation of Sections 3.4, 20.1 and 25 uses Darwin-only
files behind the existing platform interfaces, with no cgo requirement.
Both amd64 and arm64 are supported. Compatibility `Other*` stubs remain
available to callers that explicitly request an unsupported backend.

macOS uses the same XDG directories as Linux: `XDG_DATA_HOME`,
`XDG_CONFIG_HOME` and `XDG_CACHE_HOME`, falling back to `~/.local/share`,
`~/.config` and `~/.cache`, each with an `excalibur` child. Shared config,
automation and CLI code already resolve these locations, so using
`~/Library/Application Support` would split the CLI and daemon state.
Runtime files use `XDG_RUNTIME_DIR` when set, otherwise
`filepath.Join(os.TempDir(), "excalibur-<uid>")`; there is no `/run/user`
probe. The installer creates the platform directories privately.

The vault master key uses macOS Keychain through the existing
`github.com/zalando/go-keyring` v0.2.6 dependency. Inspection of the pinned
[module source](https://github.com/zalando/go-keyring/blob/v0.2.6/keyring_darwin.go)
confirmed that Set starts `/usr/bin/security -i` and supplies the secret
through stdin. The dependency stores a versioned base64 prefix and decodes
it on Get, preserving binary keys without another codec. A configured
fallback serves unavailable keychain sessions, including missing default
keychains and disallowed interaction. A missing item remains ErrNotFound;
it does not expose a stale fallback value. The adapter handles security's
truncated OSStatus exit codes and probes the default keychain when the
dependency conflates missing items with missing keychains. Ordinary tests
inject the backend; actual Keychain access requires
`EXCALIBUR_KEYCHAIN_TEST=1` on macOS.

Autostart is a user LaunchAgent named `com.excalibur.daemon`, with an
optional separate label for another installation. Its plist resides in
`~/Library/LaunchAgents/<label>.plist`, is atomically replaced with mode
0644, and starts the executable with `daemon start`. `RunAtLoad=true`,
`KeepAlive.SuccessfulExit=false` and `ThrottleInterval=10` start it at
login and restart failures, while allowing a successful exit to stop it.
`ProcessType=Standard` preserves interactive daemon responsiveness.
Standard output and error go to separate files under the XDG data
root's `logs` directory. The plist snapshots `TMPDIR=os.TempDir()` and
PATH, and retains explicitly set XDG overrides. This ensures the daemon
and CLI use the same runtime location after launchd starts the agent.
Installation uses `launchctl bootout gui/<uid>/<label>` followed by
`launchctl bootstrap gui/<uid> <plist>`; uninstall unloads and removes it.
Only a missing job is ignored. Other supervisor failures are returned,
with the plist retained for a later login or retry. Status distinguishes
absent, installed but unloaded, inactive, and active with a PID.

The plist follows Apple's [LaunchAgent contract](https://github.com/apple-oss-distributions/launchd/blob/main/man/launchd.plist.5).
launchd has no sd_notify watchdog. The existing daemon watchdog's Ready,
Tick and Stopping operations are inert without `NOTIFY_SOCKET`, which
this LaunchAgent does not set. Restart on process failure is implemented;
detecting an unresponsive but living daemon requires a separate supervisor
health mechanism and is not claimed here.

Tool commands get their own process group. Graceful cancellation sends
SIGTERM to the group, waits up to two seconds for the entire group, and
then sends SIGKILL, including grandchildren that outlive the leader.
Darwin has no Linux Pdeathsig equivalent: children can outlive a crashed
daemon; graceful shutdown kills their groups. `syscall.Exec` replaces the
current image on Darwin, so exec's private instruction-file handoff can
remove request text from argv while retaining the PID and signal ownership.
Process protection enumerates kernel process records and reads only argv
from `kern.procargs2`, with the comm name as a fallback when access fails.

Tool processes receive the Linux-equivalent nice increment, clamped to
19, plus best-effort `PRIO_DARWIN_PROCESS` background scheduling. Pinned
CPU pool threads use `PRIO_DARWIN_THREAD` with `PRIO_DARWIN_BG`, which
reduces CPU, disk I/O and network priority according to Apple's
[resource definitions](https://github.com/apple-oss-distributions/xnu/blob/main/bsd/sys/resource.h).
Darwin's libc getpriority returns nice directly. There are no cgroups,
delegated weights or memory.max controls: Delegated returns false and
SetMemoryMax returns errors.ErrUnsupported.

Resource samples combine self getrusage CPU with current ps RSS, or ps
RSS and cumulative CPU for another PID. RSS KiB converts to bytes.
Processes counts live descendants, excluding the sampled PID, zombies
and the temporary ps observation process. OOMKills is always zero;
hw.memsize supplies physical memory for the RAM watchdog threshold.
File locking uses flock with cancellable acquisition; PTYs use creack/pty;
the deferred isolator passes commands through; vault memory locking uses
best-effort mlock/munlock. Account names and IANA timezones use the same
validation as Linux, resolving `/etc/localtime` through macOS's zoneinfo
symlink when TZ is unset.

Darwin-tagged tests cover parsing, XML rendering against a golden plist,
injected launchctl lifecycle/error behavior, subprocess group cancellation,
lock contention across processes and goroutines, PTY I/O, priority, memory
locking, installer wiring and real exec request-file handoff. Cross-vet
checks their compilation on Linux; native execution remains a macOS runner
requirement. No shared interfaces, tool definitions or eval profile pins
change in this decision. The shared installer still reports Linux cgroup
and ionice terminology on macOS; correcting its report requires a separate
shared-file change.
