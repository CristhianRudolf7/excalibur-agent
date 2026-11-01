# Budget-bound provider overload retries

The director's W13 design extends Section 19.5: provider rate limits and HTTP
5xx overloads must not terminate otherwise viable runs after a fixed attempt
count. Their retries continue inside the same gateway call until success,
caller cancellation, or the run's wall-clock deadline. Server delays take
precedence over exponential backoff; positive jitter avoids synchronized
retries. Backoff without a server delay is capped at 30 seconds.

This intentionally replaces bounded retries and eventual breaker failover for
these two failure classes. Transport failures retain bounded retries and the
existing breaker policy. A caller deadline or cancellation does not count as a
provider breaker failure. The engine persists a budget pause when the deadline
expires before or during a model turn, using a short independent persistence
context so an expired request context cannot discard that transition. Authentication, invalid requests and persistent
content failures still return immediately under the existing policies.

The adapters currently retain Retry-After but discard other reset headers.
Rather than modify connector code or the provider wire contract, model wiring
constructs the existing built-in adapters with a gateway-owned HTTP observer.
It captures only delay metadata in a per-call context, enriching a copy of the
normalized provider error. Existing registry credential resolution is reused.
Repeated gateway attempts alternate primary and fallback credentials when a
distinct fallback exists. Streaming retains the previous pre-emission fallback
behavior; the engine uses non-streaming gateway calls.

Gateway configuration adds DisableBudgetRetries; operator wiring exposes
EXCALIBUR_BUDGET_RETRIES=0 to restore bounded retries and registry construction.
The default is enabled. Additive model-call record fields expose actual wait
count and elapsed wait seconds, including interrupted waits. No shared core
interface or wire schema changes, dependencies, or credential logging are needed.

The alternative of modifying every provider's header parser was rejected by
the task's connector constraint. Replaying whole engine steps was rejected
because it would consume step budgets and could duplicate tool effects.

Integration with W12 retains both state contracts: a deadline persists the
engine's budget pause, while standalone exec reports `stopped_by_timeout` and
exit code 4. The integration regression checks the journal pause and the CLI
outcome together; delegated tasks retain their shared state mapping under
decision 0063. This reconciles the wave test's outdated CLI expectation
without changing either runtime behavior.
