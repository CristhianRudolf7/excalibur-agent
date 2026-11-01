# 0060: Task deadlines and external termination records

The director's deadline design extends Sections 7.3, 17.5, 18.4 and
23.2. A task's external timeout must govern pacing rather than an unrelated
arm default. `EXCALIBUR_DEADLINE` defaults on and can disable this extension.

The explicit CLI timeout wins, followed by the first valid Harbor/operator
timeout environment variable, an instruction matching “You have N
seconds/minutes/hours”, and the existing configured budget. Multiple matching
instruction limits use the shortest positive duration. Invalid, nonpositive,
and overflowing durations are ignored. The deadline starts at entry to `RunExec`, including setup. The full external duration is retained;
closing time is reserved inside it instead of multiplying the limit by 0.9.
With the switch off, existing time-budget resolution remains unchanged.

At 50%, 75% and 90%, the engine adds one volatile pacing message per threshold.
Messages crossed during one action are delivered together at the next model
boundary. The 75% message requires convergence, and the 90% message asks for
the best deliverable and `finish`. There is no attempt to interrupt a model
response to inject a message.

A long shell command cannot be identified reliably from its text. Closing mode
therefore rejects explicitly long wait budgets and new background commands,
and bounds other new exec processes to five seconds or the remaining time,
whichever is shorter. The existing Linux terminal shell is preserved and uses the system
`timeout` command with TERM and a one-second KILL grace. This also bounds a
session after the initial exec response returns. Interactive input that could
start commands is refused; reading, signaling and closing sessions remain
available. This is the closest enforceable interpretation of “forbids starting
long commands”; a keyword denylist would miss scripts and reject useful short
checks. No new Go dependencies are introduced.

SIGTERM enters the existing CLI finalization path with a cancellation cause.
The final event, journal and final document use `stopped_by_timeout`, reason
`timeout`, and exit 143. This is a CLI record label, not a new engine lifecycle
state: shared engine transition contracts remain unchanged. Ordinary deadline expiry finalizes as paused with reason `timeout`, even when
a model call is in flight. SIGINT retains its existing canceled behavior. SIGKILL cannot be caught or recorded. No second
recorder or competing finalization goroutine is introduced. Deadline-enabled
terminal starts use the additive `SessionManager.StartContext` method so
cancellation releases the initial yield immediately; legacy `Start` keeps its
existing behavior. This prevents a long yield from delaying final records.

Integration with the later final-record contract preserves exit 4 and
`stopped_by_timeout` for standalone exec cancellation and expiry. Delegated
tasks retain shared durable paused/canceled states. This supersedes the
branch-local exit 143 and ordinary-expiry paused labels above; deadline
pacing, source selection and command bounds remain active.
