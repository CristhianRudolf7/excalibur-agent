package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCodexProviderLayers(t *testing.T) {
	file := filepath.Join(t.TempDir(), "credentials.json")
	if err := os.WriteFile(file, []byte(`{"provider":"codex","model":"test-model","reasoning_effort":"low"}`), 0600); err != nil {
		t.Fatal(err)
	}
	empty := func(string) string { return "" }
	for _, tc := range []struct {
		name string
		cfg  ModelsConfig
		path string
		env  string
		want bool
	}{
		{"operator file", ModelsConfig{}, file, "", true},
		{"config", ModelsConfig{Provider: "codex"}, "", "", true},
		{"config wins", ModelsConfig{Provider: "stub"}, file, "", false},
		{"environment wins", ModelsConfig{Provider: "codex"}, file, "stub", false},
		{"environment selects", ModelsConfig{Provider: "stub"}, file, " CODEX ", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			get := empty
			if tc.env != "" {
				get = func(k string) string {
					if k == "EXCALIBUR_PROVIDER" {
						return tc.env
					}
					return ""
				}
			}
			got, err := UsesCodex(tc.cfg, tc.path, get)
			if err != nil || got != tc.want {
				t.Fatalf("selection: %v %v", got, err)
			}
		})
	}
	cfg, err := Load(LoadOptions{SkipFiles: true, Env: []string{"EXCALIBUR_MODELS__PROVIDER=codex", "EXCALIBUR_MODELS__MODEL=test-model"}})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := UsesCodex(cfg.Models, "", empty); err != nil || !got {
		t.Fatalf("layered config did not select account: %v %v", got, err)
	}
}
