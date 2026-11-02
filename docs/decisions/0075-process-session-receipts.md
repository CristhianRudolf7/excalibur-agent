# Process session receipts

Execution results add an optional `sessions` field containing process identity,
liveness and exit status. Activity and verification history preserve the same
receipt without changing provider messages or API contracts. This additive tool
result metadata supports Sections 7.5 and 8.6: process ownership must not be
inferred from arbitrary narrative text. Current session authority and tombstones
remain authoritative. Old histories use a process-tool-only compatibility path.

`EXCALIBUR_TYPED_SESSION_EVIDENCE` defaults on and gates use and production of
these receipts. No connector or model protocol changes are required.

The optional `ExecuteOptions.CallID` field also carries observation identity into
the existing output store. Under `EXCALIBUR_MASK_IDEMPOTENT`, the embedded store
binds that identity to immutable original blobs, including results small enough
to remain inline initially. Masked model projections preserve original activity
text for auditing. `Tool`, `ToolRunner` and `OutputStore` interfaces are unchanged.
