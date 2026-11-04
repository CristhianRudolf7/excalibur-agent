package attempts

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func testMap() WorkdirMap {
	return WorkdirMap{Real: mapFixture("/app"), Attempt: mapFixture("/tmp/attempts/attempt-0/app")}
}

func TestWorkdirMapPath(t *testing.T) {
	m := testMap()
	cases := map[string]string{
		"/app":              "/tmp/attempts/attempt-0/app",
		"/app/src/main.go":  "/tmp/attempts/attempt-0/app/src/main.go",
		"/app/a/../b.txt":   "/tmp/attempts/attempt-0/app/b.txt",
		"/application/x":    "/application/x",
		"/app2/x":           "/app2/x",
		"/etc/hosts":        "/etc/hosts",
		"/app/../etc/hosts": "/app/../etc/hosts",
		"relative/x.txt":    "relative/x.txt",
		"":                  "",
	}
	for in, want := range cases {
		if got := m.Path(mapFixture(in)); got != mappedFixturePath(want) {
			t.Fatalf("Path(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWorkdirMapResolveRelative(t *testing.T) {
	m := testMap()
	if got := m.Resolve("src/x.py"); got != filepath.FromSlash(mapFixture("/tmp/attempts/attempt-0/app/src/x.py")) {
		t.Fatalf("Resolve relative = %q", got)
	}
	if got := m.Resolve(mapFixture("/app/x.py")); got != filepath.FromSlash(mapFixture("/tmp/attempts/attempt-0/app/x.py")) {
		t.Fatalf("Resolve absolute = %q", got)
	}
}

func TestWorkdirMapCommandWordBoundaries(t *testing.T) {
	m := testMap()
	cases := map[string]string{
		"cd /app && make":            "cd /tmp/attempts/attempt-0/app && make",
		"cp /app/x.bin /app/out/":    "cp /tmp/attempts/attempt-0/app/x.bin /tmp/attempts/attempt-0/app/out/",
		"echo /application":          "echo /application",
		"python3 /app.py":            "python3 /app.py",
		"ls '/app'":                  "ls '/tmp/attempts/attempt-0/app'",
		"test -f /app":               "test -f /tmp/attempts/attempt-0/app",
		"x=/app/y; cat $x":           "x=/tmp/attempts/attempt-0/app/y; cat $x",
		"make -C ./src && ls /app2":  "make -C ./src && ls /app2",
		"cat /tmp/attempts/app/x":    "cat /tmp/attempts/app/x",
		"no path here":               "no path here",
		"/app/tests/run.sh | tee /x": "/tmp/attempts/attempt-0/app/tests/run.sh | tee /x",
	}
	for in, want := range cases {
		if got := m.Command(mapFixture(in)); got != mapFixture(want) {
			t.Fatalf("Command(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWorkdirMapMapArgs(t *testing.T) {
	m := testMap()
	attempt := "/tmp/attempts/attempt-0/app"
	cases := []struct {
		name string
		tool string
		in   string
		want string
	}{
		{"write absolute path", "write_file", `{"path":"/app/out.txt","content":"hi"}`, `{"content":"hi","path":"` + attempt + `/out.txt"}`},
		{"content text untouched", "write_file", `{"path":"notes.md","content":"see /app/readme"}`, `{"content":"see /app/readme","path":"` + attempt + `/notes.md"}`},
		{"relative path resolves under attempt", "read_file", `{"path":"src/a.go"}`, `{"path":"` + attempt + `/src/a.go"}`},
		{"nested ops path", "apply_patch", `{"ops":[{"path":"/app/a.txt","op":"add"}]}`, `{"ops":[{"op":"add","path":"` + attempt + `/a.txt"}]}`},
		{"path list", "grep", `{"paths":["/app/a","/other/b"]}`, `{"paths":["` + attempt + `/a","/other/b"]}`},
		{"exec command mapped", "exec", `{"command":"cd /app && make"}`, `{"command":"cd ` + attempt + ` && make","workdir":"` + attempt + `"}`},
		{"exec explicit workdir kept", "exec", `{"command":"ls","workdir":"/app/sub"}`, `{"command":"ls","workdir":"` + attempt + `/sub"}`},
		{"exec relative workdir", "exec", `{"command":"ls","workdir":"sub"}`, `{"command":"ls","workdir":"` + attempt + `/sub"}`},
		{"exec outside workdir kept", "exec", `{"command":"ls","workdir":"/usr"}`, `{"command":"ls","workdir":"/usr"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := m.MapArgs(c.tool, json.RawMessage(mapFixture(c.in)))
			if !jsonEqual(t, got, mapFixtureJSON(t, c.want)) {
				t.Fatalf("MapArgs(%s, %s) = %s, want %s", c.tool, c.in, got, c.want)
			}
		})
	}
}

func TestWorkdirMapMapArgsPassThrough(t *testing.T) {
	m := testMap()
	// Nothing to rewrite: byte-identical.
	in := json.RawMessage(mapFixture(`{"path":"/other/x","n":12345678901234567890}`))
	if got := m.MapArgs("read_file", in); string(got) != string(in) {
		t.Fatalf("unchanged args must stay byte-identical: %s", got)
	}
	// Not an object: passes through for the tool to report.
	bad := json.RawMessage(`[1,2`)
	if got := m.MapArgs("read_file", bad); string(got) != string(bad) {
		t.Fatalf("malformed args must pass through: %s", got)
	}
	// Disabled map never rewrites.
	off := WorkdirMap{}
	if got := off.MapArgs("exec", json.RawMessage(`{"command":"ls /app"}`)); string(got) != `{"command":"ls /app"}` {
		t.Fatalf("disabled map rewrote args: %s", got)
	}
}

func TestWorkdirMapDisabledForRootAndSelf(t *testing.T) {
	if (WorkdirMap{Real: "/", Attempt: "/tmp/a"}).Enabled() {
		t.Fatal("the filesystem root must never be mapped")
	}
	if (WorkdirMap{Real: "/app", Attempt: "/app"}).Enabled() {
		t.Fatal("a map onto itself is a no-op and must be disabled")
	}
	if (WorkdirMap{Real: "app", Attempt: "/tmp/a"}).Enabled() {
		t.Fatal("relative real paths are not mapped")
	}
}

// jsonEqual compares two JSON documents semantically, so key order and
// spacing do not matter.
func jsonEqual(t *testing.T, got json.RawMessage, want string) bool {
	t.Helper()
	var a, b any
	if err := json.Unmarshal(got, &a); err != nil {
		t.Fatalf("got is not JSON: %s", got)
	}
	if err := json.Unmarshal([]byte(want), &b); err != nil {
		t.Fatalf("want is not JSON: %s", want)
	}
	ca, _ := json.Marshal(a)
	cb, _ := json.Marshal(b)
	return string(ca) == string(cb)
}

// mapFixture gives the simulated absolute paths an explicit volume on Windows.
func mapFixture(s string) string {
	if runtime.GOOS != "windows" {
		return s
	}
	return regexp.MustCompile(`(^|[\s"'=])(/(?:app|tmp|other|usr|etc))`).ReplaceAllString(s, "${1}C:${2}")
}

func mapFixtureJSON(t *testing.T, s string) string {
	t.Helper()
	var obj map[string]any
	if err := json.Unmarshal([]byte(mapFixture(s)), &obj); err != nil {
		t.Fatal(err)
	}
	var normalize func(map[string]any)
	normalize = func(m map[string]any) {
		for k, v := range m {
			switch value := v.(type) {
			case string:
				if isPathKey(k) && strings.HasPrefix(value, mapFixture("/tmp/attempts/")) && value != testMap().Attempt {
					m[k] = filepath.FromSlash(value)
				}
			case []any:
				for i, item := range value {
					if nested, ok := item.(map[string]any); ok {
						normalize(nested)
					}
					if path, ok := item.(string); ok && isPathKey(k) && strings.HasPrefix(path, mapFixture("/tmp/")) {
						value[i] = filepath.FromSlash(path)
					}
				}
			}
		}
	}
	normalize(obj)
	b, err := json.Marshal(obj)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func mappedFixturePath(s string) string {
	if strings.HasPrefix(s, "/tmp/attempts/") {
		return filepath.FromSlash(mapFixture(s))
	}
	return mapFixture(s)
}
