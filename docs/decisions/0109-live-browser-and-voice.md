# 0109: Live browser view and live voice calls

## Context

The web interface needs to show what the agent does in its browser and to hold
voice calls with low latency. Both move binary media continuously, and both need
values that must never reach the model or durable storage.

## Decisions

- Media uses dedicated same-origin WebSockets: `/ws/browser`, `/ws/voice/listen`
  and `/ws/voice/speak`. It never enters the event bus, so video and audio cannot
  delay or close the event socket, and nothing is replayed or written to telemetry.
  Viewer lifetime equals socket lifetime, which starts and stops the screencast.
- Metadata uses two additive ordinary events, `browser.session` and
  `browser.activity`, and one RPC, `browser.live_state`. They carry no typed values
  and no URL query strings or fragments.
- Live browser behavior is attached from the daemon through
  `Engine.SetLiveObserver` and a tool wrapper. Tool definitions, the capability
  manifest and the evaluation profile are unchanged, and an unwatched engine with
  no observer behaves exactly as before.
- `credentials.save_login` is an additive, value-bearing RPC, handled like
  `credentials.submit`. It reuses the remembered-bundle format, so existing secure
  fill by reference applies unchanged.
- The daemon proxies live voice instead of handing ephemeral credentials to the
  browser, keeping the credential chain in one place.
- Live hearing has no server turn detection because its transcription model
  rejects it. The client decides the end of a turn and commits.
- Live speech uses streaming synthesis rather than a speech-to-speech model. Its
  latency is comparable, and it reads the agent's text exactly.

## Consequences

The takeover remains unimplemented; the view is watch-only by construction. See
`docs/development/browser-live.md` and `docs/development/voice-live.md`.
