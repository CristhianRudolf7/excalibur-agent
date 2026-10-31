// Package memory implements Section 10: memory layers, identity
// documents and the notebook, memory records with valid time and
// expiry, adaptable entities and relations, admission rules, the daily
// background review, fused retrieval with uncertainty, embeddings via
// the provider, the document library with exact citations, and the
// memory tools.
//
// English-only mapping of the Section 10 vocabulary: kind values are
// fact, preference, event, commitment, belief, relation and procedure;
// statuses are candidate, active, expired, superseded, disputed and
// retired; trust values are user, agent and external. Relation-type
// origins are seed and learned; relation-type statuses are proposed,
// active and merged. The SQL schema in internal/store/migrations
// already carries these tables; this package owns their behavior.
package memory
