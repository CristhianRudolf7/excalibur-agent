package legacy

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/docextract"
	"github.com/CristhianRudolf7/excalibur-agent/internal/office/create"
	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"golang.org/x/sys/windows"
)

func TestWindowsOfficeLookup(t *testing.T) {
	for _, variable := range []string{"ProgramFiles", "ProgramFiles(x86)"} {
		t.Run(variable, func(t *testing.T) {
			t.Setenv("ProgramFiles", t.TempDir())
			t.Setenv("ProgramFiles(x86)", t.TempDir())
			want := filepath.Join(os.Getenv(variable), "LibreOffice", "program", "soffice.exe")
			if err := os.MkdirAll(filepath.Dir(want), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(want, nil, 0600); err != nil {
				t.Fatal(err)
			}
			got, err := windowsOfficeExecutable(func(string) (string, error) { return "", exec.ErrNotFound })
			if err != nil || got != want {
				t.Fatalf("%q %v; want %q", got, err, want)
			}
		})
	}
	for _, name := range []string{"soffice.exe", "soffice.com"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("PATH", dir)
			want := filepath.Join(dir, name)
			if err := os.WriteFile(want, nil, 0600); err != nil {
				t.Fatal(err)
			}
			got, err := officeExecutable()
			if err != nil || got != want {
				t.Fatalf("%q %v; want %q", got, err, want)
			}
		})
	}
}

func TestWindowsLegacyConversion(t *testing.T) {
	for _, format := range []string{"doc", "xls", "ppt"} {
		t.Run(format, func(t *testing.T) {
			target := map[string]string{"doc": "docx", "xls": "xlsx", "ppt": "pptx"}[format]
			spec := create.Spec{Title: "Legacy", Blocks: []create.Block{{Kind: "paragraph", Text: "hello"}}, Sheets: []create.Sheet{{Name: "Sheet1", Cells: []create.Cell{{Ref: "A1", Value: "hello"}}}}, Slides: []create.Slide{{Kind: "title", Title: "hello"}}}
			pkg, err := create.Build(target, spec, nil)
			if err != nil {
				t.Fatal(err)
			}
			data, err := pkg.Bytes()
			if err != nil {
				t.Fatal(err)
			}
			fixture := filepath.Join(t.TempDir(), "native."+target)
			if err := os.WriteFile(fixture, data, 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("EXCALIBUR_OFFICE_HELPER", "convert")
			t.Setenv("EXCALIBUR_OFFICE_FIXTURE", fixture)
			extractor := &Extractor{Format: format, Binary: os.Args[0]}
			doc, err := extractor.Extract(context.Background(), strings.NewReader("legacy"), 6, docextract.Options{})
			if err != nil || doc.Format != format || len(doc.Pages) == 0 {
				t.Fatalf("%+v, %v", doc, err)
			}
		})
	}
}

func TestWindowsLegacyCancellationKillsTree(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	t.Setenv("EXCALIBUR_OFFICE_HELPER", "wait")
	t.Setenv("EXCALIBUR_OFFICE_PID", pidFile)
	extractor := &Extractor{Format: "doc", Binary: os.Args[0]}
	done := make(chan error, 1)
	safego.Go("test/office-job", nil, func() {
		_, err := extractor.Extract(ctx, strings.NewReader("legacy"), 6, docextract.Options{})
		done <- err
	})
	var data []byte
	for ctx.Err() == nil {
		data, _ = os.ReadFile(pidFile)
		if len(data) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	err := <-done
	if len(data) == 0 {
		t.Fatal("helper did not write child PID")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	state, err := windows.WaitForSingleObject(handle, 5000)
	if err != nil || state != windows.WAIT_OBJECT_0 {
		t.Fatalf("child survived: state %d, %v", state, err)
	}
}

// TestMain also acts as the deterministic converter in child test processes.
func TestMain(m *testing.M) {
	switch os.Getenv("EXCALIBUR_OFFICE_HELPER") {
	case "convert":
		var output, target, profile string
		for i, arg := range os.Args[1:] {
			if strings.HasPrefix(arg, "-env:UserInstallation=") {
				profile = strings.TrimPrefix(arg, "-env:UserInstallation=")
			}
			if arg == "--outdir" && i+2 < len(os.Args) {
				output = os.Args[i+2]
			}
			if arg == "--convert-to" && i+2 < len(os.Args) {
				target = os.Args[i+2]
			}
		}
		if output == "" || target == "" || profile != officeFileURL(filepath.Join(output, "profile"), true) {
			os.Exit(2)
		}
		data, err := os.ReadFile(os.Getenv("EXCALIBUR_OFFICE_FIXTURE"))
		if err != nil {
			os.Exit(3)
		}
		if err := os.WriteFile(filepath.Join(output, "input."+target), data, 0600); err != nil {
			os.Exit(4)
		}
		os.Exit(0)
	case "wait":
		child := exec.Command(os.Args[0])
		child.Env = append(os.Environ(), "EXCALIBUR_OFFICE_HELPER=child")
		if err := child.Start(); err != nil {
			os.Exit(5)
		}
		if err := os.WriteFile(os.Getenv("EXCALIBUR_OFFICE_PID"), []byte(strconv.Itoa(child.Process.Pid)), 0600); err != nil {
			os.Exit(6)
		}
		time.Sleep(time.Minute)
		os.Exit(0)
	case "child":
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	os.Exit(m.Run())
}
