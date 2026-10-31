// Package fake stands in for the engine package (Sections 7, 15):
// scripted runners the orchestrator drives in embedded-mode tests.
// The real engine implements orchestrator.Runner; embedded tests use
// FakeRunner to simulate steps, tools in flight, safe-point acks and
// interrupts without any model call.
package fake
