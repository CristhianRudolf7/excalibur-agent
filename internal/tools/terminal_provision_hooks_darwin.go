package tools

import (
	"os"
	"os/exec"
)

func platformCommandPlan(dep *MissingDep, lookup LookPathFunc) (*InstallPlan, bool) {
	return nil, false
}
func platformSudoAllowed() bool { return os.Geteuid() != 0 }
func pythonSyntaxCommand() []string {
	return []string{"python3", "-c", "import ast, sys; ast.parse(sys.stdin.read(), '<write_file>')"}
}
func syntaxCheckLookPath(name string) (string, error) { return exec.LookPath(name) }
