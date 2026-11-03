package robotlab

import (
	"context"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestChildEnvironmentAllowlist(t *testing.T) {
	for name, value := range map[string]string{
		"ROBOTLAB_SENTINEL_SECRET": "private", "OPENAI_API_KEY": "private",
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
		for _, name := range []string{"ROBOTLAB_SENTINEL_SECRET", "OPENAI_API_KEY", "PYTHONPATH", "PYTHONHOME", "LD_PRELOAD"} {
			if _, ok := env[name]; ok {
				t.Fatalf("install=%v inherited %s", install, name)
			}
		}
		for _, name := range []string{"HTTP_PROXY", "https_proxy", "NO_PROXY", "PIP_INDEX_URL", "SSL_CERT_FILE"} {
			if _, ok := env[name]; ok != install {
				t.Fatalf("install=%v unexpected presence for %s", install, name)
			}
		}
		if env["LC_MESSAGES"] != "C" || env["EXCALIBUR_ROBOTLAB_TEST"] != "enabled" || env["MUJOCO_GL"] != "osmesa" {
			t.Fatal("required runtime environment missing")
		}
	}
}

func TestInstallRunnerChildEnvironment(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux installer")
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
	t.Setenv("OPENAI_API_KEY", "private")
	s := fakeSidecar(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := s.Call(ctx, "ping", nil); err != nil {
		t.Fatal(err)
	}
}

// The protocol helper calls this inside the actual supervised child.
func checkChildSecrets() {
	for _, name := range []string{"ROBOTLAB_SENTINEL_SECRET", "OPENAI_API_KEY"} {
		if _, exists := os.LookupEnv(name); exists {
			os.Exit(6)
		}
	}
}
