# Extensions

The public SDK is [`pkg/extsdk`](../pkg/extsdk). The CLI can scaffold, validate,
install, enable, disable and inspect local extensions with `excalibur ext`.
See [extension development](../docs/development/extending.md).

| Directory | Contents |
| --- | --- |
| `example-builtin/` | Built-in route manifest, definition and conformance probes. |
| `example-package/` | Tool-package manifest, tool definition and probes. |
| `example-mcp/` | MCP example server, manifest and probes. |
| `example-app/` | Contextual app manifest and probes. |
| `robot-ur/` | UR robot Python bridge, setup script, URSim tooling and e2e tests. |

Classroom, Robotics Lab and Robot appear in the web app's bundled catalog;
its install toggles control local visibility. They do not download extensions
from a production remote store. The [robot setup guide](../docs/development/robot-ur-setup.md)
describes hardware prerequisites and simulator verification limits.
