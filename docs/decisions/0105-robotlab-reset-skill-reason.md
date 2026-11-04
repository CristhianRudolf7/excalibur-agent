# 0105: Robot Lab reset skill exception

Extend section 5 `attempt.reset` parameters with optional `no_skill_reason`, a
string limited to 2000 characters. `session` and `reflection` remain required.
The source schema generates the Go parameters, JSON registry, embedded validator,
web types and `attempt_reset` tool schema. Forward the reason unchanged.

The runtime owns the rule requiring a skill written or improved in the current
failed or abandoned attempt before resetting, with an explicit reason as the
exception. Go validates the parameter shape and length without duplicating
runtime state checks. Fake sidecar tests cover omission, forwarding, character
limits and invalid types.

The tool description makes skills central to the six-step training loop, including
reflection and a concrete lesson after every failed or abandoned attempt, skill
use before acting, fresh observations before fine manipulation, and a final
summary of created or improved skills and success rates. Continuation relays
repeat the skill-writing reminder. Tests enforce the description's ordering and
3500-character limit and the exact relay text.

Verification passed with the shared-machine memory scope and process priority:

- `go build -p 2 ./...` and `go vet -p 2 ./...`.
- `go test -race -p 2 ./internal/robotlab/... ./internal/daemon/... ./internal/api/...`.
- `GOOS=darwin` and `GOOS=windows` builds with `go build -p 2 ./...`.
- Web `pnpm exec vitest run src/lib/rpc` (23 tests) and `pnpm exec tsc -b`.
- Contract regeneration followed by Prettier produced no differences; changed
  files passed formatting checks, and the goroutine check passed.
