# Compatible tool routing and terminal output retrieval

The director requested compatible routing for Sections 13.2–13.5 and 15.4–15.6:
`call_tool` may execute registered core tools as well as deferred tools, omitted
subagent actions follow a delegation card or run reference, and terminal session
references may be read through `get_tool_output`. These are explicit departures
from strict deferred-only invocation and blob-only retrieval. The alternative
was to reject the call and explain the correct tool; that consumes another
model turn for a deterministic repair.

All invocation still uses the standard executor. Recursive `call_tool` is
rejected. Eval checks the wrapped target and its task operations against the
profile before execution, preserving its existing restrictions. Card presence
takes precedence over a run reference when inferring an omitted action; explicit
actions always win. Missing information still produces a short usage example.
The `GetToolOutputTool.Sessions` collaborator is additive; stored-output lookup
retains precedence, edit-history lookup remains available, and paging remains
byte based.

The non-interactive profile now includes `exec_io` in its core tool list. Its
existing profile hash captures that change. The default-ON environment switch
`EXCALIBUR_TOOL_FRICTION_REPAIR` restores the prior routing/catalog behavior when
disabled. Existing `EXCALIBUR_TOOL_CALL_NAME` continues to control nested name
promotion; the new repair additionally accepts a nested arguments envelope.

Section 22 and Section 23.2 records gain additive `friction_error` categories on
failed tool calls and `friction_error_count` / `friction_errors` in `final.json`.
The final summary is reconstructed from durable tool records, including merged
attempt records, so reopening the recorder does not lose counts. This audit
instrumentation is unconditional to make both switch settings comparable.
Command failures are excluded. No dependency or shared core interface changes
are required.
