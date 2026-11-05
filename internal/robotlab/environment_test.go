package robotlab

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestChildEnvironmentAllowlist(t *testing.T) {
	if runtime.GOOS == "linux" && mujocoGL() != "osmesa" {
		t.Fatal("Linux runtime must use OSMesa")
	}
	for name, value := range map[string]string{
		"ROBOTLAB_SENTINEL_SECRET": "private", "HARNESS_TEST_CREDENTIAL": "private",
		"PYTHONPATH": "/untrusted", "PYTHONHOME": "/untrusted",
		"LD_PRELOAD": "/untrusted.so", "HTTP_PROXY": "http://proxy.invalid",
		"https_proxy": "http://proxy.invalid", "NO_PROXY": "localhost",
		"PIP_INDEX_URL": "https://index.invalid", "SSL_CERT_FILE": "/ca.pem",
		"LC_MESSAGES": "C", "EXCALIBUR_ROBOTLAB_TEST": "enabled",
	} {
		t.Setenv(name, value)
	}
	for _, install := range []bool{false, true} {
		env := make(map[string]string)
		for _, entry := range childEnvironment(install) {
			name, value, _ := strings.Cut(entry, "=")
			env[name] = value
		}
		for _, name := range []string{"ROBOTLAB_SENTINEL_SECRET", "HARNESS_TEST_CREDENTIAL", "PYTHONPATH", "PYTHONHOME", "LD_PRELOAD"} {
			if _, ok := env[name]; ok {
				t.Fatalf("install=%v inherited %s", install, name)
			}
		}
		for _, name := range []string{"HTTP_PROXY", "https_proxy", "NO_PROXY", "PIP_INDEX_URL", "SSL_CERT_FILE"} {
			if _, ok := env[name]; ok != install {
				t.Fatalf("install=%v unexpected presence for %s", install, name)
			}
		}
		if env["LC_MESSAGES"] != "C" || env["EXCALIBUR_ROBOTLAB_TEST"] != "enabled" || env["MUJOCO_GL"] != mujocoGL() {
			t.Fatal("required runtime environment missing")
		}
	}
}

func TestInstallRunnerChildEnvironment(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("Unix installer environment")
	}
	t.Setenv("ROBOTLAB_SENTINEL_SECRET", "private")
	t.Setenv("PIP_ROBOTLAB_TEST", "allowed")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := (execRunner{}).Run(ctx, "/bin/sh", "-c", `test -z "${ROBOTLAB_SENTINEL_SECRET+x}" && test "$PIP_ROBOTLAB_TEST" = allowed && test "$OMP_NUM_THREADS" = 2 && printf clean`)
	if err != nil || out != "clean" {
		t.Fatalf("installer environment: %q %v", out, err)
	}
}

func TestSidecarChildEnvironment(t *testing.T) {
	t.Setenv("ROBOTLAB_SENTINEL_SECRET", "private")
	t.Setenv("HARNESS_TEST_CREDENTIAL", "private")
	s := fakeSidecar(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := s.Call(ctx, "ping", nil); err != nil {
		t.Fatal(err)
	}
}

// The protocol helper calls this inside the actual supervised child.
func checkChildSecrets() {
	for _, name := range []string{"ROBOTLAB_SENTINEL_SECRET", "HARNESS_TEST_CREDENTIAL"} {
		if _, exists := os.LookupEnv(name); exists {
			os.Exit(6)
		}
	}
}

func TestRuntimePathsAndRenderingEnvironment(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("EXCALIBUR_ROBOT_SKILLS_DIR", "")
	if !strings.HasSuffix(SkillsDir(), filepath.Join(".local", "share", "excalibur", "robot-skills")) {
		t.Fatal(SkillsDir())
	}
	t.Setenv("EXCALIBUR_ROBOT_SKILLS_DIR", "/skills")
	t.Setenv("EXCALIBUR_ROBOTLAB_MENAGERIE", "/assets")
	t.Setenv("MUJOCO_GL", "egl")
	env := strings.Join(childEnvironment(false), "\n")
	for _, want := range []string{"EXCALIBUR_ROBOT_SKILLS_DIR=/skills", "EXCALIBUR_ROBOTLAB_MENAGERIE=/assets", "EXCALIBUR_ROBOTLAB_MENAGERIE=/assets"} {
		if !strings.Contains(env, want) {
			t.Fatal(env)
		}
	}
	if runtime.GOOS == "linux" && !strings.Contains(env, "MUJOCO_GL=egl") || SkillsDir() != "/skills" {
		t.Fatal(env)
	}
}
