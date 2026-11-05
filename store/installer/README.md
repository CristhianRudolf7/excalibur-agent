# Store installer directory

This directory is reserved for remote store tooling; it contains no installer
implementation. `excalibur ext install` installs local extension directories
through the Go CLI. External skills and tool packages have separate validation
paths. A general remote catalog/install/publish backend is not implemented.
See [current store support](../../docs/store/README.md).
