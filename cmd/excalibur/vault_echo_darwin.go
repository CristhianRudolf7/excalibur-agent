package main

import (
	"os"
	"os/exec"
)

// setEcho toggles terminal echo through stty; other platforms without
// stty report an error and callers fall back to --stdin.
func setEcho(on bool) error {
	arg := "-echo"
	if on {
		arg = "echo"
	}
	cmd := exec.Command("stty", arg)
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func disableEcho() (func(), error) {
	if err := setEcho(false); err != nil {
		return nil, err
	}
	restored := false
	return func() {
		if !restored {
			restored = true
			_ = setEcho(true)
		}
	}, nil
}
