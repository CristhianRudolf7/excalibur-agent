# Live voice calls

The web call mode hears the user with a streaming transcription session and
speaks with streaming synthesis. The conversational agent stays the only brain:
transcribed text enters as a normal agent turn with all its tools, and speech
output reads the agent's text verbatim.

## Modes

`VoiceMode` in `apps/web/src/features/chat/voice/call.ts`:

| Mode | Hearing | Speech | Used when |
| --- | --- | --- | --- |
| `realtime` | `/ws/voice/listen` | `/ws/voice/speak` | `voice.status` is available and the listen socket reports `ready` |
| `browser` | Web Speech recognition | Web Speech synthesis | No credentials, a listen failure, or a credential failure mid-call |
| `server` | Recorded segments, `voice.transcribe` | `voice.speak` | Only when `realtime` fails and the browser has no speech recognition |

Falling back is automatic and is never shown as an error. A failure at the start
of a call switches silently, since nothing was said yet. Later failures show the
existing discreet `callFallback` notice. A synthesis failure alone switches speech
to Web Speech synthesis and keeps `realtime` hearing. A listen socket that drops
mid-call with anything other than `unavailable` reconnects once before falling
back.

## Design decisions, from measurements with the real service (2026-11-04)

- **The daemon proxies; no ephemeral secrets.** The browser never receives a
  credential, the credential chain stays in one place, and failures can move to
  the fallback key before the client notices. Sockets stay same-origin, so they
  work through the same proxies as `/ws`.
- **Hearing:** `gpt-realtime-whisper` streams words while the user speaks; the
  first word arrived about 1.1 s after audio began. It **rejects server turn
  detection** ("Turn detection is not supported for this transcription model"),
  so `turn_detection` is `null` and the client decides the end of a turn, then
  commits. The final text arrives about 0.35 s after the commit. Models that
  accept server turn detection deliver no words until the user stops speaking.
- **Speech:** streaming `/audio/speech` as PCM with the `marin` voice. First audio
  arrived in 0.52-0.63 s, against 0.48 s for the fastest speech-to-speech model
  with a socket already open and 0.77-0.81 s for the larger one. Synthesis reads
  the text exactly; a speech-to-speech model can paraphrase or answer it, which is
  unacceptable when reading an agent's results.
- **Exhausted credentials:** the upstream socket connects and reports
  `session.created`, and only then sends an `error` event of type
  `insufficient_quota`. A session therefore counts as ready only after
  `session.updated`, and that window is where the fallback key is tried.

## Daemon API

Package `internal/voice`, using the existing `Credentials` and
`FallbackCredentials` chain. Values come from `config.VoiceConfig`, with new
optional fields `live_stt_model` (default `gpt-realtime-whisper`) and
`live_voice` (default `marin`).

```go
type ListenEvent struct {
	Type string // "delta", "final", "closed", "error"
	Item string
	Text string
	Code string // for "error": "unavailable" or "failed"
}

// Listen returns once the upstream session confirmed its configuration.
func (c *Client) Listen(ctx context.Context, language string) (*ListenSession, error)
func (s *ListenSession) Append(pcm []byte) error // PCM16LE, mono, 24 kHz
func (s *ListenSession) Commit() error
func (s *ListenSession) Clear() error
func (s *ListenSession) Events() <-chan ListenEvent
func (s *ListenSession) Close() error

// SpeakStream delivers PCM16LE mono 24 kHz chunks as they arrive.
func (c *Client) SpeakStream(ctx context.Context, text string, write func([]byte) error) error
```

`ErrUnavailable` means that no credential works: none is configured, the primary
and fallback are both rejected, or voice is disabled.

## Gateway sockets

Registered in `internal/gateway/voice_live.go` through an optional backend
interface. Same rules as `/ws/browser`: loopback peer check, default same-origin
`Accept`, HTTP 404 without the interface. Audio and transcripts are never logged
or stored, and never reach the event bus.

### `GET /ws/voice/listen?language=es`

At most 2 concurrent sessions (HTTP 429). `language` is a lower-case two-letter
code, defaulting to `es`.

- Client binary message: PCM16LE, mono, 24 kHz, at most 64 KiB.
- Client text: `{"type":"commit"}`, `{"type":"clear"}`.
- Server text: `{"type":"ready"}`,
  `{"type":"delta","item":"…","text":"…"}`,
  `{"type":"final","item":"…","text":"…"}`,
  `{"type":"error","code":"unavailable"|"failed"}`.

A commit with too little audio yields `{"type":"final","item":"","text":""}`
rather than an error. After `error` the server closes the socket with status 1000.
An upstream close mid-call yields `{"type":"error","code":"failed"}`. A session
that cannot start yields `error`, not an HTTP failure, so the client always learns
the reason in the same way.

### `GET /ws/voice/speak`

At most 2 concurrent sockets (HTTP 429), one synthesis at a time per socket.

- Client text: `{"type":"speak","id":7,"text":"…"}` with a positive integer `id`
  and 1 to 4096 characters, `{"type":"cancel","id":7}`. A new `speak` cancels the
  previous one.
- Server binary: `uint32` big-endian `id`, then PCM16LE mono 24 kHz bytes.
- Server text: `{"type":"done","id":7}`,
  `{"type":"error","id":7,"code":"unavailable"|"failed"}`.

The `id` prefix lets the client discard audio that was in flight when it cancelled.

## Client behavior

- Captures through an `AudioWorklet` in a 24 kHz `AudioContext`, with
  `echoCancellation`, `noiseSuppression` and `autoGainControl` requested.
- A voice gate saves upstream use and avoids transcribing silence. Audio is sent
  only from a voice-activity start, with 400 ms of pre-roll, to the end of the
  turn. Muting clears the gate.
- Partial words appear live as captions. Recognized words, not bare energy,
  interrupt a pending reply, and text matching what is being spoken is ignored as
  echo, like the existing browser mode.
- At the end of a turn the client commits and waits up to 1.5 s for the final
  text, then falls back to the partial text. Empty text starts no turn.
- Replies wait up to 13 minutes, since browser turns can take 12.
- Speech is scheduled gaplessly with Web Audio. Barge-in stops it at once.
- While the agent works, short acknowledgements are spoken, never stating results.
- The call overlay minimizes on its own when the live browser panel opens or a
  secure credential form is requested; the compact bar keeps the call controls
  and the live caption next to the panel.

## Recording with a fake microphone

```sh
google-chrome --use-fake-ui-for-media-stream --use-fake-device-for-media-stream \
  --use-file-for-fake-audio-capture=/path/to/voice.wav%noloop
```

The WAV must be PCM. A virtual PulseAudio source also works, without those flags:
it is an ordinary microphone to Chrome, so the same echo cancellation applies.

## Verification

`go test -race ./internal/voice ./internal/gateway ./internal/daemon
./internal/config` use a fake Realtime server. `pnpm exec vitest run
src/features/chat/voice` covers call state, recognition, synthesis and fallback.
`e2e/voice-live.spec.ts` and `e2e/voice-call.spec.ts` use Chrome's fake audio
device with mocked sockets.

A full call with the real service used Chrome's fake microphone with a recorded
WAV, at 1440 and 390 pixels: live captions, a real agent turn that used its browser
tools, spoken acknowledgements and the spoken reply, with the live browser panel
beside the compact call bar. With an exhausted credential, the call switched to
browser speech with no error or notice.

Transcription accuracy is the main practical risk. Across repeated real runs, the
same recorded sentence had an acronym and a domain word heard differently, which
changed where the agent went or whether it used its browser tools at all. Prefer
plain words to acronyms, and rehearse recorded phrases. The assistant is told that
transcripts can distort saved login names, but a garbled request can still lead
it elsewhere. Saved credentials stay safe either way, since they are bound to
their exact login host.
