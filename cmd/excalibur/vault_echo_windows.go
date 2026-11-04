package main

import (
	"os"
	"sync"

	"golang.org/x/sys/windows"
)

func disableEcho() (func(), error) {
	handle := windows.Handle(os.Stdin.Fd())
	var original uint32
	if err := windows.GetConsoleMode(handle, &original); err != nil {
		return nil, err
	}
	if err := windows.SetConsoleMode(handle, original&^windows.ENABLE_ECHO_INPUT); err != nil {
		return nil, err
	}
	var once sync.Once
	return func() { once.Do(func() { _ = windows.SetConsoleMode(handle, original) }) }, nil
}
