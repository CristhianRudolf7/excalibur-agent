package store

import (
	"context"
)

// Family is the per-user file set of Section 4.2 behind one handle:
// the durable main database, the NORMAL-durability telemetry
// database, and the shared content-addressed blob store. Each file
// keeps its own porter (one writer goroutine per file, Section 4.3).
type Family struct {
	layout Layout
	Main   *DB
	Tele   *DB
}

// OpenFamily opens (creating) the whole Section 4.2 family under
// root: the main database with mainReg, the telemetry database with
// teleReg, blobs/ beside them. Callers pass migrations.Main() and
// migrations.Telemetry(); the store package never imports the schema
// so the dependency points one way.
func OpenFamily(ctx context.Context, root string, mainOpts Options, mainReg MigrationRegistry, teleOpts Options, teleReg MigrationRegistry) (*Family, error) {
	layout := NewLayout(root)
	if err := EnsureLayout(layout); err != nil {
		return nil, err
	}
	mainOpts.BlobDir = layout.BlobsDir()
	main, err := Open(ctx, layout.MainPath(), mainOpts, mainReg)
	if err != nil {
		return nil, err
	}
	teleOpts.BlobDir = layout.BlobsDir()
	tele, err := Open(ctx, layout.TelemetryPath(), teleOpts, teleReg)
	if err != nil {
		_ = main.Close()
		return nil, err
	}
	return &Family{layout: layout, Main: main, Tele: tele}, nil
}

// Layout returns the file family layout.
func (f *Family) Layout() Layout { return f.layout }

// Close shuts every file down.
func (f *Family) Close() error {
	var first error
	if f.Tele != nil {
		if err := f.Tele.Close(); err != nil && first == nil {
			first = err
		}
	}
	if f.Main != nil {
		if err := f.Main.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}
