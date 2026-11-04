package tools

import (
	"os/exec"
	"strings"
)

func platformSudoAllowed() bool { return false }

func platformCommandPlan(dep *MissingDep, lookup LookPathFunc) (*InstallPlan, bool) {
	name := commandWordBase(dep.Name)
	type packageIDs struct{ winget, scoop, choco, verify string }
	ids, ok := map[string]packageIDs{
		"git":     {"Git.Git", "git", "git", "git --version"},
		"python":  {"Python.Python.3.13", "python", "python", "python --version"},
		"python3": {"Python.Python.3.13", "python", "python", "python --version"},
		"node":    {"OpenJS.NodeJS.LTS", "nodejs-lts", "nodejs-lts", "node --version"},
		"nodejs":  {"OpenJS.NodeJS.LTS", "nodejs-lts", "nodejs-lts", "node --version"},
		"go":      {"GoLang.Go", "go", "golang", "go version"},
		"jq":      {"jqlang.jq", "jq", "jq", "jq --version"},
		"rg":      {"BurntSushi.ripgrep.MSVC", "ripgrep", "ripgrep", "rg --version"},
		"ripgrep": {"BurntSushi.ripgrep.MSVC", "ripgrep", "ripgrep", "rg --version"},
	}[name]
	if !ok {
		return nil, true
	}
	for _, manager := range []string{"winget", "scoop", "choco"} {
		if _, err := lookup(manager); err != nil {
			continue
		}
		var pkg, command string
		switch manager {
		case "winget":
			pkg = ids.winget
			command = "winget install --id " + pkg + " -e --accept-source-agreements --accept-package-agreements"
		case "scoop":
			pkg = ids.scoop
			command = "scoop install " + pkg
		case "choco":
			pkg = ids.choco
			command = "choco install " + pkg + " -y --no-progress"
		}
		return &InstallPlan{Manager: manager, Package: pkg, Commands: []string{command}, Verify: ids.verify}, true
	}
	return nil, true
}

func syntaxCheckLookPath(name string) (string, error) {
	p, err := exec.LookPath(name)
	// The App Execution Alias starts the Store instead of a Python parser.
	if err == nil && (commandWordBase(name) == "python" || commandWordBase(name) == "python3") && strings.Contains(strings.ToLower(strings.ReplaceAll(p, `\`, "/")), "/microsoft/windowsapps/") {
		return "", exec.ErrNotFound
	}
	return p, err
}

func pythonSyntaxCommand() []string {
	if _, err := syntaxCheckLookPath("py"); err == nil {
		return []string{"py", "-3", "-c", "import ast, sys; ast.parse(sys.stdin.read(), '<write_file>')"}
	}
	return []string{"python", "-c", "import ast, sys; ast.parse(sys.stdin.read(), '<write_file>')"}
}
