# Tolerant nested arguments and protection from accidental self-signals

Architecture references: Sections 3.2, 13.4, 13.5 and Appendix B.

The director requested deterministic repair of JSON strings in container
parameters, range clamping with visible notes, and protection against terminal
commands accidentally terminating the harness. Schema-directed normalization
runs before validation, including before existing alias normalization, and
again afterward so aliases and scalar conversions receive the same treatment.
Repairs appear in the tool result and its journal record. Invalid JSON strings,
wrong container types and missing required parameters still fail validation.
No subagent action is inferred from an incomplete request.

Terminal launches inspect shell syntax before executing any command effects.
Literal kill selectors are checked against the current harness, its parent,
and members of both process groups. Pattern selectors are matched against
protected names or argv. Matching commands are refused with an explicit PID
remediation. Unknown selector options and dynamic signal arguments are refused
conservatively because evaluating them could itself execute effects. This is
an intentional departure from accepting every valid shell command: rejecting
ambiguous cleanup is preferable to killing the execution journal owner. The
alternative, executing pgrep then rewriting a complex shell program, changes
shell semantics and creates additional races.

This is an accident guard, not the deferred security sandbox. It inspects
literal shell calls and nested literal shell command strings; arbitrary
programs, sourced scripts, dynamically constructed command names, and process
changes after inspection are outside its guarantee. Linux process inspection
and re-exec stay in internal/platform; other platforms return ErrUnsupported.
The additive helpers do not change the shared platform interfaces.

The Linux exec entry point re-execs the same binary and PID with a short argv
placeholder, transferring its request through a private environment variable
that is cleared before tools run. This preserves signals and exit status while
removing task text from the harness argv. Parent launchers can still contain
that text, so the command guard remains necessary.

Switches: EXCALIBUR_TOOL_TOLERANT_ARGS controls schema repairs;
EXCALIBUR_SELF_KILL_GUARD controls terminal protection and argv transfer.
Both default on. Connectors and their invocation contracts remain untouched.

The guard also refuses killall executable-path selectors and conservatively
matches long killall names against Linux's truncated 15-byte process names.
Otherwise a longer executable name could evade a comparison against comm.
