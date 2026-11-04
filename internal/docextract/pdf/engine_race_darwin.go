//go:build darwin && race

package pdf

import (
	"time"

	"github.com/tetratelabs/wazero"
)

func init() {
	// Under race instrumentation, cold native compilation of PDFium exceeds
	// the 1 GiB worker RSS budget on macOS. Race builds use the interpreter,
	// which keeps Wasm limits and cancellation; release builds keep wazero's
	// native compiler for throughput.
	newPDFRuntimeConfig = wazero.NewRuntimeConfigInterpreter
	// A cold interpreted render under race instrumentation can outlast the
	// release per-operation bound on slower runners.
	workerOperationTimeout = 3 * time.Minute
}
