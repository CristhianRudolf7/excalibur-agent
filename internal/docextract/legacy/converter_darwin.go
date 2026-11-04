package legacy

import "os/exec"

func officeExecutable() (string, error) {
	binary, err := exec.LookPath("libreoffice")
	if err != nil {
		binary, err = exec.LookPath("soffice")
	}
	return binary, err
}
func officeProfileURL(p string) string     { return officeFileURL(p, false) }
func runOfficeCommand(cmd *exec.Cmd) error { return cmd.Run() }
