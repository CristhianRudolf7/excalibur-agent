# example-builtin (route 4: in-process Go)

A tool implemented in Go and registered with `ProvideBuiltin`.
In-process tools use the same interface and manifest as every other
route: there is no privileged path. `builtin.json` holds the test
fixture for `excalibur ext test .`.
