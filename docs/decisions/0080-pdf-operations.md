# Real PDF operations through the existing deferred tool

The document tool previously changed text trailers rather than PDF objects.
Use the Apache-2.0 pdfcpu v0.11.1 library for structural PDF operations under
Sections 13, 14.7 and 17. The existing `PDFBackend` interface is unchanged;
`PDFOperationsBackend` adds advanced operations without breaking injected
backends. `MediaDeps` adds optional reading hooks, workdir confinement and
private password-reference resolution. `docextract` contracts are unchanged.

Keep the existing deferred tool name and add actions and arguments to its
schema. No connector, API contract or frozen eval tool-table changes are
needed. Write new output files unless replacement is explicitly requested.
Publish complete temporary files atomically, with no-clobber publication by
default, and return digests and actual page counts. Operations are bounded
and cancellation takes effect before publication, after a library call ends.
Split publishes individual files and may leave completed outputs if a later
one fails; its result is not a multi-file transaction.

Passwords can be arguments or vault references. Resolve references under
`pdf_ops` with the current project/delegation policy; never expose password
or resolver diagnostics in outputs. Prefer references because tool intent
records retain caller arguments. Disable local pdfcpu configuration loading
and networking to keep results independent of operator settings. Form locking
sets read-only flags without regenerating appearances or loading user fonts.
Flattening, XFA, signing and secure redaction fail explicitly.

The PDF skill now describes the implemented operations and their limits.
Preserve its original bytes in a frozen eval snapshot and use
`skills.NewEvalBuiltinIndex` for eval profile hashing and eval execution.
Default/delegate indexes use the current skill. The eval hash/table pins are
unchanged; the snapshot retains the existing baseline metadata verbatim.
This small additive skill-index seam is required because hashing the rewritten
assistant skill would otherwise change eval's profile fingerprint.

The tool declares non-idempotent effects conservatively: explicit in-place
rotation or stamping must not be repeated after an uncertain crash. It does
not advertise the file-history inverse because PDF writes are not recorded
by the file-edit history service. Output digests and page counts support
operator verification without claiming an automatic recovery probe.

Verification exposed a tight guard in `TestEvalTimeOnlySurvivesPast25Steps`:
the untouched baseline took 14.08 seconds against its 15-second context
limit on the shared host, and the updated build exceeded that limit. Align
the test context with the 60-second run budget it already configures. Its
step-count, completion and prompt assertions are unchanged; this is a bounded
test guard adjustment, not a production timeout or eval-profile change.
