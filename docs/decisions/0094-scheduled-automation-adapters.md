# Scheduled automation RPC and daemon adapters

The additive `automations.*` RPC family uses the shared automation Service
contract. It exposes list, get, runs, run, pause, resume and delete; creation,
versioned updates and restoration stay on the agent tool. Tool create/update
require explicit conversation approval. Existing shared interfaces are unchanged.

Automation events use the existing telemetry bus, websocket filters and replay.
Run notices use Master.SendAssistant, which persists history before publishing.
The primary web conversation is `cnv_web_excalibur`.

Agent steps run synchronously through a fresh engine run, using the model gateway,
context compiler, journal and normal executor pipeline. This returns final answer
text directly and honours cancellation without polling detached orchestrator work.
Unattended calls exclude questions, automation recursion and generic dispatch or
delegation that could bypass those restrictions. Other registered tools retain
their executor validation, redaction, timeout and result handling.

The default and assistant catalogs gain automations. The eval profile and eval
catalog remain unchanged; only default catalog hashes are intentionally updated.
The manager construction is deferred to the BE1 merge factory described in the
backend milestone document.
