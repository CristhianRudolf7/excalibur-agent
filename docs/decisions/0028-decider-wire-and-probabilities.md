# 0028: Decider wire shape and probability sources (Sections 19.3-19.4)

Date: 2026-10-31. Area: `internal/models` (H12, milestone w3).

## What

The architecture (19.4) fixes the decider interface — state plus
typed `choice`/`score`/`noul` questions returning values with
calibrated probability — and names three interchangeable
implementations (Jev API, pplx-decider API or local weights, LLM
fallback), but not the vendor wire bytes or the local sidecar
protocol.

## Decision

- The HTTP backends (`NewJevDecider`, `NewPplxDecider`) speak one
  shared JSON shape (`state`, `questions:[{id,kind,prompt,options,
  context}]` to `answers:[{id,choice,score,probability}]`) against
  configurable base URLs, so the hosted Jev endpoint, the pplx API
  and a local 4B sidecar URL are all the same client with different
  URLs. Exact vendor field names verify at integration; simulated
  servers in tests speak this shape.
- The LLM fallback takes its probability from the model's own
  restricted-JSON number, clamped to [0,1]. The H1 adapters expose
  no `logprobs`, so there is no second source yet; when a future
  adapter exposes logprobs the fallback can combine them without
  changing the interface.
- The fallback sets no `reasoning_effort` itself. The gateway role
  entry fixes effort per role for cache stability (19.2) and
  ReasonOmit contracts (DeepSeek, Ollama) drop it on the wire;
  embedding operations never carry it (`DecisionsConfig`).

## Alternative

Vendor SDKs per backend with exact native fields. Rejected: three
SDKs for one seam add surface for two products dated September and
October 2026 with little history (uncertainty 27.4); the shared
shape keeps the seam swappable and the discrepancy local.

## Calibration

Thresholds start at low 0.35 / high 0.75 with per-port constants
(ask 0.7, stuck 0.9) and calibrate on the harness's own labeled
records (`Calibrate`, needs >= 10 samples). Vendor calibration
claims are not trusted, per 19.4.
