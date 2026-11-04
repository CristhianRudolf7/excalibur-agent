package platform

import (
	"encoding/xml"
	"strings"
	"testing"
)

func TestWindowsDaemonCommand(t *testing.T) {
	for _, tc := range []struct{ path, want string }{
		{`C:\Program Files\Excalibur\excalibur.exe`, `"C:\Program Files\Excalibur\excalibur.exe" daemon start`},
		{`C:\a&b\excalibur.exe`, `"C:\a&b\excalibur.exe" daemon start`},
		{`C:\trailing\`, `"C:\trailing\\" daemon start`},
		{`a"b`, `"a\"b" daemon start`},
	} {
		if got := windowsDaemonCommand(tc.path); got != tc.want {
			t.Errorf("command = %q; want %q", got, tc.want)
		}
	}
}
func TestWindowsTaskXML(t *testing.T) {
	var task struct {
		Triggers struct {
			LogonTrigger struct {
				UserID string `xml:"UserId"`
			}
		}
		Principals struct {
			Principal struct {
				RunLevel  string
				LogonType string
			}
		}
		Settings struct {
			RestartOnFailure struct {
				Interval string
				Count    int
			}
			ExecutionTimeLimit string
		}
		Actions struct {
			Exec struct{ Command, Arguments string }
		}
	}
	path := `C:\A & B\excalibur.exe`
	if err := xml.Unmarshal([]byte(windowsTaskXML(path, "S-1-5-21-123")), &task); err != nil {
		t.Fatal(err)
	}
	if task.Actions.Exec.Command != path || task.Actions.Exec.Arguments != "daemon start" {
		t.Fatalf("action = %+v", task.Actions)
	}
	if task.Principals.Principal.RunLevel != "LeastPrivilege" || task.Principals.Principal.LogonType != "InteractiveToken" {
		t.Fatal("task is not limited per-user")
	}
	if task.Triggers.LogonTrigger.UserID != "S-1-5-21-123" || task.Settings.RestartOnFailure.Count != 255 || task.Settings.ExecutionTimeLimit != "PT0S" {
		t.Fatal("missing logon or supervisor settings")
	}
}
func TestWindowsTaskStatus(t *testing.T) {
	for _, tc := range []struct{ in, want string }{{"Status: Running\r\n", "active"}, {"Status: Ready", "inactive"}, {"Status: Disabled", "disabled"}, {"localized: unknown", "installed"}} {
		if got := windowsTaskStatus(strings.ReplaceAll(tc.in, "\n", "\n  ")); got != tc.want {
			t.Errorf("status = %s", got)
		}
	}
}
