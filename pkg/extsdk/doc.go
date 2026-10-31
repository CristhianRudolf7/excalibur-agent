// Package extsdk is the public SDK for building Excalibur extensions
// (Section 14.12). It is deliberately self-contained: it never imports
// internal/ packages, so out-of-tree extensions can depend on it.
//
// Built-in tools implement the same shapes through internal/tools;
// the parity test in internal/tools asserts the two stay convertible
// via JSON.
package extsdk
