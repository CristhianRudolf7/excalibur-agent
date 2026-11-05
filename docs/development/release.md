# Public release engineering

## Native CI corrections

The logs for runs 41843594333 (macOS) and 41843595594 (Windows) were
retrieved through the jobs API. Both native workflows now cover main pushes
and pull requests. Staticcheck is pinned to 2026.2.2.

- Google tool errors follow Go error conventions.
- Removed unused Torch helpers; the runtime no longer installs Torch separately.
- Robot Lab fake commands recognize the Windows `py -3` prefix. Path assertions
  use native separators. Unsupported-host installer tests assert rejection.
- Rendering uses CGL on macOS, no MUJOCO_GL on Windows, and the operator's
  selected backend on Linux. The macOS dependency test checks the actual pip
  requirements step. Its Python environment test now checks the real child
  environment instead of importing the removed `robotlab.sandbox` module.
- Removed `TestDarwinSandboxMemoryLimits`: it tested that removed Python
  module, which is no longer on any runtime execution path. No replacement
  Python sandbox or claim of Python memory limits is introduced here.
- Protected-path changes are tests only: `TestExecSignalRecords` allows 60
  seconds for a race-instrumented helper to initialize on Intel macOS;
  shutdown still has its original two-second assertion.
  `TestExecRateLimitsExpireWallBudget` allows 30 seconds so initialization
  does not consume the entire provider retry budget.
- The Settings focus failures in these runs predate the committed fix placing
  Passwords after Robot Lab. The current component already matches the test.
  The Windows media POSIX-mode assertion reported in those logs is also absent
  from the current branch.

Native macOS and Windows execution must be confirmed by CI; this workspace
runs Linux.
