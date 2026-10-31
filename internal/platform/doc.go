// Package platform abstracts every operating-system dependency of the
// harness behind interfaces: paths, process groups, process priority,
// resource sampling, file locks, the system keyring, the service
// manager, PTYs and the Isolator extension point where deferred
// security (Section 20) will connect.
//
// Linux is the only implemented platform in this version; other
// platforms arrive as new files behind the same interfaces without
// touching the core. Every interface has its Linux implementation in
// *_linux.go (minimal compiling placeholders in wave 0; the real
// implementation is milestone H2) and a *_other.go stub
// (//go:build !linux) returning errors.ErrUnsupported.
//
// Architecture reference: Section 3.4 (platform table) and principle
// P10. Implemented in milestones H0 (contracts, wave 0) and H2 (Linux).
package platform
