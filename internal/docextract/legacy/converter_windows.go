package legacy

import (
	"os"
	"os/exec"
	"path/filepath"
)

func officeExecutable() (string, error) { return windowsOfficeExecutable(exec.LookPath) }

func windowsOfficeExecutable(lookup func(string) (string, error)) (string, error) {
	for _, name := range []string{"soffice.exe", "soffice.com"} {
		if binary, err := lookup(name); err == nil {
			return binary, nil
		}
	}
	for _, variable := range []string{"ProgramFiles", "ProgramFiles(x86)"} {
		if root := os.Getenv(variable); root != "" {
			binary := filepath.Join(root, "LibreOffice", "program", "soffice.exe")
			if st, err := os.Stat(binary); err == nil && st.Mode().IsRegular() {
				return binary, nil
			}
		}
	}
	return "", exec.ErrNotFound
}

func officeProfileURL(p string) string { return officeFileURL(p, true) }
