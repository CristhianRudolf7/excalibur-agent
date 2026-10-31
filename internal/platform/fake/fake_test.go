package fake

import (
	"os/exec"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
)

func TestFakePlatform(t *testing.T) {
	paths, err := TempPaths("exc-test-")
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{paths.Data, paths.Config, paths.Cache, paths.Runtime} {
		if dir == "" {
			t.Fatal("empty path")
		}
	}
	if len(paths.ExtensionsDir()) != 1 {
		t.Fatal("extensions dir")
	}

	pg := &ProcessGroup{}
	pg.Configure(exec.Command("true"))
	if err := pg.Kill(123); err != nil {
		t.Fatal(err)
	}

	pp := &ProcessPriority{}
	if err := pp.LowerProcess(1); err != nil {
		t.Fatal(err)
	}
	if err := pp.LowerCurrentThread(); err != nil {
		t.Fatal(err)
	}

	sampler := &ResourceSampler{Next: platform.ResourceSample{RSSBytes: 10}}
	sample, err := sampler.Sample(0)
	if err != nil || sample.RSSBytes != 10 {
		t.Fatal("sample")
	}

	lock := &FileLock{}
	release, err := lock.Acquire("/tmp/x.lock")
	if err != nil {
		t.Fatal(err)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}

	kr := NewKeyring()
	if err := kr.Set("svc", "u", []byte("s3cr3t")); err != nil {
		t.Fatal(err)
	}
	got, err := kr.Get("svc", "u")
	if err != nil || string(got) != "s3cr3t" {
		t.Fatal("keyring roundtrip")
	}
	if _, err := kr.Get("svc", "nobody"); err == nil {
		t.Fatal("expected missing entry error")
	}

	sm := &ServiceManager{}
	if err := sm.Install(); err != nil {
		t.Fatal(err)
	}
	if st, _ := sm.Status(); st != "installed" {
		t.Fatal("status")
	}

	pty := &PTY{}
	sess, err := pty.Start(exec.Command("true"), platform.Winsize{Rows: 24, Cols: 80})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sess.Write([]byte("ls\n")); err != nil {
		t.Fatal(err)
	}
	sess.(*PTYSession).Out.WriteString("out")
	buf := make([]byte, 3)
	if n, _ := sess.Read(buf); n != 3 {
		t.Fatal("pty read")
	}
	if err := sess.Resize(platform.Winsize{Rows: 40, Cols: 100}); err != nil {
		t.Fatal(err)
	}
	if err := sess.Wait(); err != nil {
		t.Fatal(err)
	}
	if err := sess.Close(); err != nil {
		t.Fatal(err)
	}

	iso := &Isolator{}
	if err := iso.Isolate(exec.Command("true")); err != nil {
		t.Fatal(err)
	}
}
