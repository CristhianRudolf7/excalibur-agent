# Image input for document vision

Sections 10.9 and 14.4 require real page images at the model boundary.
`providers.Message.Images` additively carries image URLs, including base64 data
URLs. OpenAI Responses serializes these as `input_image` parts alongside
`input_text`. Text-only requests retain their existing wire representation.
OpenAI Chat Completions rejects image requests rather than dropping them.
Other provider adapters remain unchanged; the vision backend admits only the
OpenAI Responses provider until other adapters implement image transport.

Vision uses its own models gateway with three total attempts, no fallback
chain and a per-call deadline, so the general gateway's budget retry policy
cannot turn document OCR into unbounded retries. Eval keeps its existing
backend and frozen tool definitions.

`EXCALIBUR_VISION_MODEL` selects an explicit vision-role model on the configured
provider; omission uses the default model with a warning. Existing single-model
role construction remains unchanged. Both daemon and embedded tool composition
accept optional `MediaDeps`; daemon memory and opt-in embedded memory bind
`memory.VisionToolAdapter` through `VisionMemoryOption`. Embedded exec does not
create a persistent memory store by default, so the added `MemoryStore` option
provides that seam to callers without imposing storage on eval.
