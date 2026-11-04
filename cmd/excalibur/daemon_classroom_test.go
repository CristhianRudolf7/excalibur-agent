package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
)

var stopClassroomDaemon = func(cmd *exec.Cmd) error { return cmd.Process.Signal(syscall.SIGTERM) }

// Boot the command's production composition, including provider HTTP encoding,
// model gateway, intake, chat tool selection and the durable classroom service.
func TestDaemonClassroomContextThroughModelHTTP(t *testing.T) {
	if os.Getenv("EXCALIBUR_CLASSROOM_DAEMON_HELPER") == "1" {
		t.Setenv("EXCALIBUR_PROVIDER", "openai")
		t.Setenv("EXCALIBUR_CREDENTIALS_FILE", os.Getenv("EXCALIBUR_CLASSROOM_CREDS"))
		if err := runDaemonStart([]string{"--listen", "127.0.0.1:0", "--data-dir", os.Getenv("EXCALIBUR_CLASSROOM_DATA")}); err != nil {
			t.Fatal(err)
		}
		return
	}
	observed := make(chan []string, 4)
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
			Input []struct {
				Type string `json:"type"`
			} `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		text := `{"choice":"conversation","probability":1}`
		var names []string
		for _, tool := range body.Tools {
			names = append(names, tool.Name)
		}
		var output any
		if len(names) > 0 {
			select {
			case observed <- names:
			default:
			}
			toolResult := false
			for _, input := range body.Input {
				toolResult = toolResult || input.Type == "function_call_output"
			}
			if slices.Contains(names, "classroom") && !toolResult {
				output = []any{map[string]any{"type": "function_call", "call_id": "create", "name": "classroom", "arguments": `{"action":"create_course","title":"Git foundations","goal":"Use Git locally","level":"beginner","language":"en","units":[{"title":"Basics","topics":[{"title":"Commits","summary":"Record changes"}]}]}`}}
			} else {
				text = "Prepared."
			}
		}
		if output == nil {
			output = []any{map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": text}}}}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "response", "status": "completed", "output": output, "usage": map[string]any{"input_tokens": 10, "output_tokens": 10}})
	}))
	defer model.Close()
	root := t.TempDir()
	creds := filepath.Join(root, "credentials.json")
	if err := os.WriteFile(creds, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	log, err := os.Create(filepath.Join(root, "daemon.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	cmd := exec.Command(os.Args[0], "-test.run=^TestDaemonClassroomContextThroughModelHTTP$")
	cmd.Stdout, cmd.Stderr = log, log
	for _, env := range os.Environ() {
		if !strings.HasPrefix(env, "EXCALIBUR_") && !strings.HasPrefix(env, "OPENAI_") && !strings.HasPrefix(env, "XDG_") {
			cmd.Env = append(cmd.Env, env)
		}
	}
	cmd.Env = append(cmd.Env, "EXCALIBUR_CLASSROOM_DAEMON_HELPER=1", "EXCALIBUR_CLASSROOM_CREDS="+creds, "EXCALIBUR_CLASSROOM_DATA="+filepath.Join(root, "data"),
		"EXCALIBUR_API_KEY=simulated-key", "EXCALIBUR_MODEL=classroom-test", "EXCALIBUR_BASE_URL="+model.URL, "EXCALIBUR_SERVICE_TIER=default", "EXCALIBUR_MASTER_ENGINE_TURNS=1",
		"XDG_CONFIG_HOME="+filepath.Join(root, "config"), "XDG_RUNTIME_DIR="+filepath.Join(root, "run"))
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = stopClassroomDaemon(cmd)
		// Command.Wait blocks on process exit, so bound shutdown independently.
		stop := time.AfterFunc(10*time.Second, func() { _ = cmd.Process.Kill() })
		err := cmd.Wait()
		stop.Stop()
		if err != nil {
			raw, _ := os.ReadFile(log.Name())
			t.Errorf("daemon exit: %v: %s", err, raw)
		}
	})
	var address string
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		raw, _ := os.ReadFile(log.Name())
		for _, line := range strings.Split(string(raw), "\n") {
			if strings.HasPrefix(line, "daemon listening on ") {
				address = strings.TrimPrefix(line, "daemon listening on ")
			}
		}
		if address != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if address == "" {
		raw, _ := os.ReadFile(log.Name())
		t.Fatalf("daemon did not start: %s", raw)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	rpc := func(method string, params, out any) {
		t.Helper()
		req, _ := api.NewRequest("classroom", method, params)
		raw, _ := json.Marshal(req)
		res, err := client.Post("http://"+address+"/rpc", "application/json", bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var response api.Response
		if err := json.NewDecoder(res.Body).Decode(&response); err != nil {
			t.Fatal(err)
		}
		if response.Error != nil {
			t.Fatal(response.Error)
		}
		if out != nil {
			if err := json.Unmarshal(response.Result, out); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Deliberately no study keywords: context must select tools independently.
	rpc(api.MethodConversationSend, api.ConversationSendParams{ConversationID: "cnv_web_excalibur", ClientMsgID: "create", Text: "Go ahead with the agreed plan.", Context: &api.AppContext{App: "classroom"}}, nil)
	select {
	case names := <-observed:
		for _, want := range []string{"classroom", "whiteboard", "load_skill"} {
			if !slices.Contains(names, want) {
				t.Fatalf("model did not receive %s: %v", want, names)
			}
		}
	case <-time.After(10 * time.Second):
		t.Fatal("chat model request missing")
	}
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
		var list api.ClassroomListResult
		rpc(api.MethodClassroomList, struct{}{}, &list)
		if len(list.Courses) == 1 && list.Courses[0].Title == "Git foundations" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("classroom.create_course did not persist a course")
}
