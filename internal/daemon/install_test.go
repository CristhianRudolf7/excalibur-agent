// Installer tests: 25.2 directory layout, layered config, unit and
// mechanism report.
package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeDirs struct{ root string }

func (f fakeDirs) DataDir() (string, error)    { return filepath.Join(f.root, "data"), nil }
func (f fakeDirs) ConfigDir() (string, error)  { return filepath.Join(f.root, "cfg"), nil }
func (f fakeDirs) CacheDir() (string, error)   { return filepath.Join(f.root, "cache"), nil }
func (f fakeDirs) RuntimeDir() (string, error) { return filepath.Join(f.root, "run"), nil }

type fakeUnit struct {
	installed bool
	fail      bool
}

func (f *fakeUnit) Install() error {
	if f.fail {
		return errFake("unit failed")
	}
	f.installed = true
	return nil
}

func (f *fakeUnit) Status() (string, error) {
	if f.installed {
		return "installed", nil
	}
	return "absent", nil
}

type fakeErr string

func (e fakeErr) Error() string { return string(e) }

func errFake(s string) error { return fakeErr(s) }

type fakeProber struct{ rep EnvReport }

func (f fakeProber) Probe() EnvReport { return f.rep }

func TestInstallLayoutAndConfig(t *testing.T) {
	root := t.TempDir()
	in := &Installer{
		Dirs:    fakeDirs{root},
		Service: &fakeUnit{},
		Prober:  fakeProber{rep: EnvReport{CgroupDelegated: true, NiceAvailable: true}},
	}
	rep, err := in.Install(InstallOptions{Autostart: true, Timezone: "America/Lima", Providers: []string{"openai"}, VaultMode: "secret_service"})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	for _, d := range []string{rep.DataDir, rep.ConfigDir, rep.CacheDir, rep.RuntimeDir, filepath.Join(rep.DataDir, "backups")} {
		if st, serr := os.Stat(d); serr != nil || !st.IsDir() {
			t.Errorf("missing dir %q: %v", d, serr)
		}
	}
	raw, err := os.ReadFile(rep.ConfigFile)
	if err != nil {
		t.Fatalf("config missing: %v", err)
	}
	body := string(raw)
	for _, want := range []string{"America/Lima", "secret_service", "openai"} {
		if !strings.Contains(body, want) {
			t.Errorf("config lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "sk-") || strings.Contains(body, "api_key = \"") {
		t.Errorf("config must never hold API keys:\n%s", body)
	}
	if rep.UnitStatus != "installed" {
		t.Errorf("unit status = %q, want installed", rep.UnitStatus)
	}
	if len(rep.Warnings) != 0 {
		t.Errorf("clean install must warn nothing: %v", rep.Warnings)
	}
}

func TestInstallWarnsWithoutMechanisms(t *testing.T) {
	root := t.TempDir()
	in := &Installer{Dirs: fakeDirs{root}, Service: &fakeUnit{}, Prober: fakeProber{}}
	rep, err := in.Install(InstallOptions{VaultMode: "passphrase"})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if len(rep.Warnings) < 2 {
		t.Errorf("missing mechanisms and passphrase vault must warn: %v", rep.Warnings)
	}
	if rep.UnitStatus != "not installed" {
		t.Errorf("no autostart keeps status literal: %q", rep.UnitStatus)
	}
}

func TestInstallKeepsExistingConfig(t *testing.T) {
	root := t.TempDir()
	in := &Installer{Dirs: fakeDirs{root}, Service: &fakeUnit{}}
	cfgDir := filepath.Join(root, "cfg")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	custom := "# user edits\n[user]\ntimezone = \"custom\"\n"
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte(custom), 0o600); err != nil {
		t.Fatal(err)
	}
	rep, err := in.Install(InstallOptions{Timezone: "other"})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	raw, _ := os.ReadFile(rep.ConfigFile)
	if string(raw) != custom {
		t.Errorf("reinstall must keep user edits, got:\n%s", raw)
	}
}

func TestInstallNeedsServiceForAutostart(t *testing.T) {
	in := &Installer{Dirs: fakeDirs{t.TempDir()}}
	if _, err := in.Install(InstallOptions{Autostart: true}); err == nil {
		t.Error("autostart without a service manager must fail")
	}
}
