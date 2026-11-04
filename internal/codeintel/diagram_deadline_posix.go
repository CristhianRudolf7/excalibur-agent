//go:build !windows

package codeintel

import "context"

func diagramIndexContextError(ctx context.Context) error { return ctx.Err() }
