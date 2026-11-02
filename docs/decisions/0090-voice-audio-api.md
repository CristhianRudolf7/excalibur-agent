# 0090: Client voice operations over OpenAI audio and gateway blobs

The additive `voice.status`, `voice.transcribe` and `voice.speak` client methods
use `internal/voice`, not the model tool catalog. The existing voice capability
remains deterministic; its definition, schemas, eval profile and pins are unchanged.

`[voice]` accepts `provider` (`openai`), `stt_model`, `tts_model`, `voice` and
`enabled`. Defaults are `gpt-4o-mini-transcribe`, `gpt-4o-mini-tts` and `alloy`.
An omitted `enabled` follows credential availability. Explicit false disables
operations. Models and voices remain configurable.

The daemon passes the configured model credential-file path and vault resolver.
Each audio operation reloads model settings and resolves credentials through the
provider registry (vault, environment, configured/file value). Status makes no
network call. A configured non-OpenAI model provider reports voice unavailable;
its credentials are never forwarded to OpenAI. Credentials, provider bodies and
transport error strings are excluded from returned errors. Redirects are refused.
Requests have a 90 second context deadline and HTTP timeout, and honor caller
cancellation. There are no automatic retries or paid calls in ordinary tests.

`POST /blobs` adds binary upload with calculated SHA-256, returning `audio_blob`
and `blob_digest`. Existing digest-addressed uploads remain available. Voice and
the gateway share the same persistent blob store (memory only without a database).
Speech results return the blob reference and MIME; existing
`GET /blobs/{ref}?mime={mime}` serves it with that content type. No new blob-store
interface or model-gateway protocol is introduced.

Audio input and generated audio are capped at 25 MiB. Transcription MIME types
are validated and multipart file extensions match the audio type. Speech accepts
1–4096 Unicode characters per call and mp3, opus or wav. Longer text receives
`invalid_params`; clients must split it into sentences and play successive results.
This preserves the shared contract's single-blob result.

The default transcription model supports JSON, which may omit language and
duration. In that case `language` is the requested language or empty, and
`duration_ms` is zero (unknown), never an estimated duration. A returned provider
duration is converted from seconds to milliseconds. Speech duration is omitted
because the provider returns audio bytes without timing metadata.

Defaults and supported voices/formats were checked against the official
[OpenAI speech guide](https://developers.openai.com/api/docs/guides/text-to-speech)
and [transcription guide](https://developers.openai.com/api/docs/guides/speech-to-text).
Account-specific model availability is reported as an upstream HTTP error;
operators can configure another model rather than incur an implicit retry.

Tests cover multipart and JSON request shapes, configured and default models and
voices, credential precedence and resolution at every boundary, limits, upstream
failures, redirects, cancellation, secret-safe errors, gateway blob round trips,
missing credentials and configuration persistence.
