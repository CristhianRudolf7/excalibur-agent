# 0093: Add whiteboard to the embedded catalog name pin

The owner authorized one new deferred tool, `whiteboard`, in the INT-2
whiteboard contract. It is composed through the shared embedded builder and
the daemon, with default/delegate admission and explicit eval exclusion.

Only `embeddedToolsHash` in `internal/daemon/tools_parity_test.go` changes:

- Previous names hash:
  `1c000767c03fc87941becd0077add3cdffcc682079de4d2c57a24e7b55d03e74`.
- Names hash including the additional deferred tool:
  `cb7e3eb40a79a35320c2bd4bf3ba0c94aa875e1c57d8d252490190187dec3dbd`.

The fixed prefix remains
`95b1a140c09eb43f24691ca087d359a7630592d9c147ae2fb9b84e00fc06adae`.
The eval profile remains
`6bfd22cc417b7ddefb81d2193c181dcc2406d5f951ab6a90f50d1db1f9f4e025`.
No eval table, existing tool definition or prefix pin changes. The builtin
`whiteboard-teaching` skill is excluded from the eval index, preserving its
skill fingerprint. Decision 0092 describes persistence and routing.
