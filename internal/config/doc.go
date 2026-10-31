// Package config holds the layered configuration types, defaults and
// loading: Appendix B defaults under system < user < project < scope
// TOML layers, with environment and flag overrides on top (Section
// 25.2 point 1). Implemented in milestones H0 (types and defaults,
// wave 0) and H2 (layered loading).
package config
