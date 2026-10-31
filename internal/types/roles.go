package types

// ModelRole names a usage role. Configuration assigns each role a
// primary model and a per-provider fallback chain; code never names
// concrete models (Section 19.1).
type ModelRole string

// Model roles.
const (
	RoleInterlocutor ModelRole = "interlocutor"
	RoleExecutor     ModelRole = "executor"
	RolePlanner      ModelRole = "planner"
	RoleExpert       ModelRole = "expert"
	RoleVerifier     ModelRole = "verifier"
	RoleExtractor    ModelRole = "extractor"
	RoleDecider      ModelRole = "decider"
	RoleVision       ModelRole = "vision"
	RoleEmbeddings   ModelRole = "embeddings"
	RoleReranker     ModelRole = "reranker"
	RoleSTT          ModelRole = "stt"
	RoleTTS          ModelRole = "tts"
)
