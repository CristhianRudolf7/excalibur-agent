# Validated sources on assistant messages

Assistant answers need deterministic document citations backed by retrieval in
the answering run or its descendants (Section 10.9). The master now persists the
rendered text and sources before publishing `message.created`. History returns
the same values. Internal child reports retain exact references for later master
validation; they are not rendered into independent numbering schemes.

The additive `api.SourceRef` contract carries document locations, excerpts,
original blob metadata and web URLs. `MessageEvent` and history messages gain an
optional `sources` array. Existing clients can ignore it. The event registry and
all existing event names remain unchanged; `events.json` adds payload schemas.

`EXCALIBUR_CITATIONS_PIPELINE` defaults on. It gates normalization, validation,
rendering, dropped-reference metrics and tool-dependent citation instructions.
Off preserves the original text and omits sources. Eval has no library tools and
its frozen tool, skill and profile hashes remain unchanged.

The current master has no configured production ModelCaller and conversations
use MemStore. This change supplies the output boundary and routes completed
master turns through it; it does not create a second model transport or a new
conversation database. `DaemonBackend.SendAssistant` accepts trusted run delivery
and derives the descendant tree. No assistant token stream exists today;
`memory.StreamHold` supplies bounded marker buffering for future stream adapters.
