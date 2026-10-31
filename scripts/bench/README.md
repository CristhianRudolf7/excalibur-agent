# Benchmark launchers

Harbor launchers per dataset and arm (Section 23.2, Appendix F).

- `build.sh [--out DIR] [--version TAG]`: static cgo+musl Linux
  binaries (amd64, arm64) with sha256 sidecars for task images.
- `run.sh <tb21|tb40> <provider/model> [k] [--upload] [-- extra]`:
  wraps `harbor run` with the fixed comparison conditions (pinned
  dataset, `-k 5` default, no timeout/resource changes, results stay
  local unless `--upload`).
- No paid benchmark runs without explicit user approval and budget
  (Section 23.1).
