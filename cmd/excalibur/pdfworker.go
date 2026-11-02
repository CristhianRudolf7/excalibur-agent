package main

import (
	"context"
	"fmt"
	"os"

	"github.com/CristhianRudolf7/excalibur-agent/internal/docextract/pdf"
)

// The private worker is deliberately absent from commands() and global help.
// Intercept it before the ordinary command dispatcher or model wiring runs.
func init() {
	if len(os.Args) != 3 || os.Args[1] != "internal" || os.Args[2] != "pdf-worker" {
		return
	}
	if err := pdf.ServeWorker(context.Background(), os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}
