//go:build linux && race

package pdf

// Cold PDFium compilation in the race-instrumented Excalibur worker exceeds
// 1 GiB RSS, including the race runtime's shadow memory. Keep compiler coverage
// with a bounded allowance for instrumentation; release builds retain 1 GiB.
// Explicit Options.MemoryLimit values and the Wasm linear-memory cap are unchanged.
const defaultWorkerMemoryLimit uint64 = 2 << 30
