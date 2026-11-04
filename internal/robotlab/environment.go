package robotlab

import (
	"os"
	"path/filepath"
	"strings"
)

// childEnvironment keeps daemon credentials out of the runtime and its children.
// Installation additionally needs pip configuration, proxies and CA paths.
func childEnvironment(install bool) []string {
	env := make([]string, 0)
	for _, entry := range os.Environ() {
		name, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		allowed := false
		switch name {
		case "MUJOCO_GL", "EXCALIBUR_ROBOT_SKILLS_DIR", "PATH", "HOME", "USER", "LANG", "TZ", "TMPDIR", "XDG_RUNTIME_DIR":
			allowed = true
		}
		allowed = allowed || platformEnvAllowed(name) || strings.HasPrefix(name, "LC_") || strings.HasPrefix(name, "EXCALIBUR_ROBOTLAB")
		if install {
			switch name {
			case "HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "no_proxy", "all_proxy":
				allowed = true
			}
			allowed = allowed || strings.HasPrefix(name, "PIP_") || strings.HasPrefix(name, "SSL_CERT_")
		}
		if allowed {
			env = append(env, entry)
		}
	}
	// Fixed values are not inherited: PYTHONPATH/PYTHONHOME can inject host code.
	if gl := mujocoGL(); os.Getenv("MUJOCO_GL") == "" && gl != "" {
		env = append(env, "MUJOCO_GL="+gl)
	}
	env = append(env, "PYTHONUNBUFFERED=1", "OMP_NUM_THREADS=2", "MKL_NUM_THREADS=2", "PYTHONDONTWRITEBYTECODE=1")
	if install {
		env = append(env, "PIP_DISABLE_PIP_VERSION_CHECK=1")
	}
	return env
}

// SkillsDir returns the session skill root shared with the runtime.
func SkillsDir() string {
	if root := os.Getenv("EXCALIBUR_ROBOT_SKILLS_DIR"); root != "" {
		return root
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "excalibur", "robot-skills")
}
