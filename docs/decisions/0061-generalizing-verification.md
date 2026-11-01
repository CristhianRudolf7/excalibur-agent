# 0061: Verify generated inputs and the explicit output contract

The director requires the strong-verification path to distinguish a working
method from agreement with its visible example. Sections 7.5, 8.7 and 18.1–18.2
provide the existing completion gate, evidence and independent review seams.

Extend the existing independent verifier call with a fresh-input assessment and
one checklist entry per explicit instruction property, including every output
format constraint. The literal request remains the source of properties; the
bounded draft extraction must not limit the checklist. Require executed checks,
an independent expected result or invariant, and evidence after the last change.
The reviewing model judges applicability and semantics; deterministic keyword
matching cannot establish input novelty or oracle independence.

`EXCALIBUR_GENERALIZE_CHECKS=on|off` defaults on and follows the existing
self-verification/strong-verifier master switches. Off restores the prior
coverage requirements, context limits and malformed-response fallback. On,
missing coverage fields, failed checklist entries and verifier call errors
return the executor to work through the existing gate. This deliberately
replaces the old fail-open fallback for this extension: an unavailable review
cannot establish the newly required checks. Existing retry and budget policies
remain authoritative. No additional model call or tool execution path is added.
The verifier output allowance is 1536 tokens when enabled (512 when off) to
accommodate the checklist. Recent command arguments and results have bounded
larger excerpts, with history indices and the last mutation boundary.

Record an observational `generalization_check` journal entry containing the
coverage report and missing checks. It has no recovery effects. No shared
interfaces, connectors, dependencies or benchmark integration change.

The alternative was detecting seeds, parser names and assertion keywords in
shell text. That would mistake mentions for checks and cannot verify arbitrary
functions, input generators or output contracts. The chosen review still has
model judgment limitations; passing coverage is evidence review, not a proof of
correctness for every possible input. Hidden tests and reference solutions are
never sought or executed.
