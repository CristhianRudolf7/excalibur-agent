# Document library storage and tool surface

Sections 10.9, 13, 14.4, 14.7 and 17 require extracted document storage,
scoped retrieval and exact citations. Migration v6 adds upload state, page
records and chunk locations without removing existing columns or data.
`memory.Service` exposes the document API agreed for the document library
round. `internal/docextract/docextract.go` is unchanged.

Extraction writes are atomic. Embedding requests run before the write, in
batches of at most 64. Identical chunk text reuses vectors from the previous
version in the active embedding space. Identical extracted content advances
the version while preserving chunk IDs and existing citation references;
page records remain versioned. Changed content retains historical chunks,
but search ranks only the current version. Page rectangles merge per physical
page, preserving locations when a chunk spans pages. Embedding failures queue
vectors for later retry and leave lexical retrieval available.

FTS triggers enrich lexical text with document title, source filename and
section path. Stored text and embedding inputs do not receive this enrichment.
A document deletion trigger removes page records even when foreign keys are
not enabled by a store connection.

The shared embedded catalog adds four deferred tools: `library_list`,
`library_view`, `library_grep` and `library_status`. Both embedded callers
and the daemon bind the same implementations to a memory service. Their
profiles are default and delegate. Eval pruning rejects their discovery and
execution. The master fast set additionally includes `library_list`.

The existing core `library_search` stub and its definition stay frozen;
its bound implementation accepts document filters and returns source text,
neighbours and the exact citation marker. Moving that existing core name to
deferral would change the frozen prefix, so it retains its existing placement.

The intentional embedded names hash changes from
`b9bedd828535af82488a5a7034279a376a3165377cdac3c87adddf7d7b4408eb`
to `9d08d0b7bd3db7d696c10c88fe4146ceab32369ca02743e9298e115ce5b60c32`.
The prefix stays
`95b1a140c09eb43f24691ca087d359a7630592d9c147ae2fb9b84e00fc06adae`;
the eval profile stays
`6bfd22cc417b7ddefb81d2193c181dcc2406d5f951ab6a90f50d1db1f9f4e025`.
The eval tool table and skill catalog are unchanged.
