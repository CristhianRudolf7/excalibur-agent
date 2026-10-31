// Package types holds the shared domain types of Excalibur: typed
// identifiers, run and task states, scopes, effect classes, model roles,
// the event envelope and the core planning structs.
//
// It is a leaf package: it must not import any other Excalibur package
// so that every other package can depend on it without cycles.
//
// Architecture reference: Sections 4.4 (identifiers), 7.2 (run states),
// 8.2-8.3 (planning data model, criterion state), 9.1 (scopes), 13.1 and
// 17.2 (effect classes), 19.1 (model roles), 22.1 (events) and Appendix A
// (logical schema). Implemented in wave 0.
package types
