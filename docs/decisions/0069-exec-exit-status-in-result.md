# 0069: Exit status in the exec tool result

The exec tool reported every finished command as a success and carried the
shell exit only as text in the header. Callers that read the tool result
(the provider `is_error` flag, the finish gate, the trial record) could not
tell a failed command from a successful one.

`tools.Result` gains one additive field, `ExitCode *int`, set by the exec
tool to the real shell exit (128+n when a signal ended the process) and nil
for tools without an exit status and for sessions still running. The
Executor and every other tool ignore it. The exec tool sets
`StatusToolError` for a non-zero or unknown exit, so `is_error` follows the
existing status mapping. The trial record reads `ExitCode` when present and
keeps the old 0/1 mapping otherwise.

With `EXCALIBUR_EXEC_ERROR_STATUS=0` the field is not set and a non-zero exit
is reported as `StatusOK`, as before. Only the optional field is added to a
shared contract; no existing field, method or interface changed.
