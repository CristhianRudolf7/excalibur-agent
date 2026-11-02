//go:build !linux

package tools

import "errors"

// Process inspection reads /proc, which only Linux provides. Other hosts
// see an empty table: session flags stay false and group signalling is
// reported as unsupported, which keeps the cross-platform builds working.

func readProcessTable() processTable { return nil }

func processHoldsTerminal(pid int) bool { return false }

func signalGroup(pgid int, kill bool) error { return errors.ErrUnsupported }

func groupAlive(pgid int) bool { return false }

func processTreeListeningTCP(root int) bool { return false }
