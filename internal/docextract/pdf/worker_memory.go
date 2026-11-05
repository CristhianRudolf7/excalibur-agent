//go:build !linux || !race

package pdf

const defaultWorkerMemoryLimit uint64 = 1 << 30
