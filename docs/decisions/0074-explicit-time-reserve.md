# 0074: Distinguish wall estimates from explicit limits

Sections 6.5, 7.3, 18.4 and 23.2 require observable non-interactive limits and
honest completion. A default estimate cannot establish the outer deadline.

Add `WallSource` to the engine profile and the optional `wall_source` field to
evaluation budget points. Existing engine callers with an empty source retain
their configured-limit semantics; CLI runs always supply `explicit` or
`default`. Explicit environment limits use the same source as explicit flags.
`EffectiveBudgets.HardWallSecs` records the enforced bound separately from the
wall estimate. No provider, gateway or external wire contracts change.

With `EXCALIBUR_TIME_RESERVE_EXPLICIT_ONLY` enabled, fallback guidance stays at
900 seconds and the internal safety bound is 3600 seconds. Both engine and CLI
use that bound, and parent cancellation remains authoritative. Fallback
reserves never automatically accept a finish. A live build or test is a finish
precondition that the two-strike advisory policy cannot waive, except for the
explicit 85% reserve. Existing service completion rules remain applicable.
