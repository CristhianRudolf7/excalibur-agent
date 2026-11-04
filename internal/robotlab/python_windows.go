package robotlab

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Resolve every PATH entry so a Store alias does not hide a real interpreter.
func findWindowsPython() (string, []string, error) {
	for _, name := range []string{"py.exe", "python.exe"} {
		for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
			if dir == "" {
				continue
			}
			candidate, err := filepath.Abs(filepath.Join(dir, name))
			if err != nil {
				continue
			}
			if strings.Contains(strings.ToLower(filepath.ToSlash(candidate)), "/microsoft/windowsapps/") {
				continue
			}
			path, err := exec.LookPath(candidate)
			if err == nil {
				if name == "py.exe" {
					return path, []string{"-3"}, nil
				}
				return path, nil, nil
			}
		}
	}
	return "", nil, errors.New("python 3 not available on Windows; install Python with the py launcher or add python.exe to PATH")
}
