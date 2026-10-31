//go:build !linux

package tools

import (
	"os"
	"syscall"
)

func sysProcAttr() *syscall.SysProcAttr { return nil }

func sigINT() os.Signal  { return os.Interrupt }
func sigTERM() os.Signal { return os.Interrupt }
func sigKILL() os.Signal { return os.Kill }
