# 0059: Repair automatic provisioning from terminal diagnostics

The director requests deterministic recovery of missing dependencies from every
terminal observation, including delayed session output and compound commands.
This extends the Section 13 tool pipeline under the existing
`EXCALIBUR_AUTOPROVISION=auto|suggest|off` policy; the default stays auto.
No connector or shared core interface changes are required.

The previous implementation guessed a missing executable from the first command
token when diagnostics did not match. Evaluation audit records contain attempts
to install shell builtins, unrelated core utilities, and a timeout argument.
Only explicit missing executable diagnostics now identify executables.
Scanning independent diagnostic lines also handles multiple missing dependencies;
install-like text in heredocs no longer disables recognition.

Session read, wait (including timeout), and list observations use the same helper
as exec. Installs are serialized, apt waits for external dpkg locks, and apt's
update cache advances only after successful execution. Suggestion and network
failure cannot mark an update completed. Package managers report actual mirror
connectivity; unrelated public DNS TCP reachability no longer gates installation.

Python module recovery selects the interpreter named in the diagnostic or command,
including virtual environment paths, with pip/uv installation and import verification
in the task directory. Missing pip first tries that interpreter's ensurepip, then
Debian's python3-pip when available, and retries once. Missing python installs
python-is-python3; missing python3 installs python3-minimal. Successful installation
is followed by executable verification and reports the exact original command for
explicit retry. Original commands are never replayed automatically because their
earlier effects may already have occurred.

Alternatives were to ask the model to repair every environment problem or replay
the failed compound command. The former leaves deterministic recovery unused;
the latter can duplicate side effects. Installation and verification failures
remain visible to the model. Unavailable package distributions, compilation
failures, privileges, and mirror outages still require an explicit next action.

The same policy also applies to status session tails: read-only auditing found
two status observations containing missing python3/file diagnostics without a
provisioning report. To honor the director's “any tool output” requirement,
status declares exec effects, non-idempotent execution, graceful cancellation,
and a 300-second timeout when sessions are attached and auto provisioning is
active. It remains read-only and fast without sessions or under off/suggest.
This is a deliberate departure from Section 13's normally fast status view;
leaving a hidden install behind read-only metadata would be incorrect.

Audit clarification: full blobs confirm successful installation in all 26 exec
observations mentioning missing python3/file. Three shortened previews hid the
report. Reports therefore precede command output, preserving the recovery result
in prefix-limited record previews. Historical stderr is retained literally.
The measured missing-tool classification alone cannot establish causation.
