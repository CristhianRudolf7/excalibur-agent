package tasks

import "os"

func artifactExecutable(path string, _ os.FileMode) bool {
	return windowsExecutableExtension(path, os.Getenv("PATHEXT"))
}
