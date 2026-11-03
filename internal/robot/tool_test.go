package robot

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

func TestToolEconomyDescription(t *testing.T) {
	for _, setting := range []string{"", "on", "0", "false", " OFF "} {
		t.Run(setting, func(t *testing.T) {
			t.Setenv("EXCALIBUR_ROBOT_TOOL_ECONOMY", setting)
			description := (&Tool{}).Definition().Description
			on := setting == "" || setting == "on"
			if strings.Contains(description, "First call look") == on {
				t.Fatalf("unexpected look instruction: %s", description)
			}
			for _, rule := range []string{"approval_required", "approved:true ONLY after", "EACH motion", "STOP is always available and needs no approval.", "Pick and place: look -> locate", "open gripper, move_pose", "close gripper, lift", "open gripper, lift clear.", "Locate computes base coordinates without moving.", "meters/radians", "Camera frames may be stale: check updated_at.", "Workspace, speed, step and mode limits are enforced by the bridge."} {
				if !strings.Contains(description, rule) {
					t.Errorf("missing safety/task rule %q", rule)
				}
			}
			if on {
				for _, rule := range []string{"look/locate when", "explicit moves", "directly", "tool checks", "result says approval is required", "post-action state"} {
					if !strings.Contains(description, rule) {
						t.Errorf("missing economy rule %q", rule)
					}
				}
			}
		})
	}
}

func TestToolApprovalRefusalExplainsNextStep(t *testing.T) {
	t.Setenv("EXCALIBUR_ROBOT_TOOL_ECONOMY", "on")
	s, f := fixture(t)
	result, err := (&Tool{Service: s}).Invoke(context.Background(), tools.Invocation{Arguments: json.RawMessage(`{"action":"move_relative","delta":{"dz":-0.05}}`)})
	if err == nil || result.Status != tools.StatusDenied {
		t.Fatalf("approval bypass: %+v %v", result, err)
	}
	for _, phrase := range []string{"approval is required", "Ask the user", "exact action", "direction, distance and speed", "approved:true", "explicit approval"} {
		if !strings.Contains(result.Output, phrase) {
			t.Errorf("refusal missing %q: %s", phrase, result.Output)
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, call := range f.calls {
		if strings.HasPrefix(call, "POST ") {
			t.Fatalf("refused action reached bridge: %s", call)
		}
	}
}

func TestToolActionResultIncludesPostActionState(t *testing.T) {
	for _, action := range []string{
		`{"action":"move_relative","delta":{"dz":-0.05}}`,
		`{"action":"move_named","name":"home"}`, `{"action":"home"}`,
		`{"action":"move_pose","pose":[0,0,0,0,0,0]}`,
		`{"action":"move_joints","joints":[0,0,0,0,0,0]}`,
		`{"action":"gripper","gripper":"open"}`,
	} {
		for _, setting := range []string{"", "off", "poll-failure"} {
			t.Run(action+setting, func(t *testing.T) {
				t.Setenv("EXCALIBUR_ROBOT_TOOL_ECONOMY", setting)
				var acted atomic.Bool
				var postPolls atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					switch r.URL.Path {
					case "/health", "/stop":
						fmt.Fprint(w, `{"ok":true}`)
					case "/state":
						z := 0.3
						if acted.Load() {
							postPolls.Add(1)
							if setting == "poll-failure" {
								http.Error(w, "state unavailable", http.StatusServiceUnavailable)
								return
							}
							z = 0.25
						}
						fmt.Fprintf(w, `{"connected":true,"approval_required":false,"tcp_pose":[0.1,0.2,%g,0,0,0],"gripper":"open"}`, z)
					case "/move", "/gripper":
						acted.Store(true)
						fmt.Fprint(w, `{"ok":true,"final_pose":[0.1,0.2,0.25,0,0,0]}`)
					default:
						t.Errorf("unexpected request %s", r.URL.Path)
						http.NotFound(w, r)
					}
				}))
				defer server.Close()
				s := New(Options{URL: server.URL, External: true, PollInterval: time.Hour})
				s.Start(context.Background())
				defer s.Close()
				// Start the bridge and seed a deliberately different pre-action state.
				if err := s.ensure(context.Background()); err != nil {
					t.Fatal(err)
				}
				if _, err := s.State(context.Background()); err != nil {
					t.Fatal(err)
				}
				result, err := (&Tool{Service: s}).Invoke(context.Background(), tools.Invocation{Arguments: json.RawMessage(action)})
				if err != nil || result.Status != tools.StatusOK {
					t.Fatalf("action: %+v %v", result, err)
				}
				var output struct {
					OK         bool      `json:"ok"`
					FinalPose  []float64 `json:"final_pose"`
					State      *State    `json:"state"`
					StateError string    `json:"state_error"`
				}
				if err := json.Unmarshal([]byte(result.Output), &output); err != nil {
					t.Fatal(err)
				}
				if !output.OK || len(output.FinalPose) != 6 || !acted.Load() {
					t.Fatalf("lost original action result: %s", result.Output)
				}
				if postPolls.Load() != 1 {
					t.Fatalf("expected to reuse the single post-action poll, got %d", postPolls.Load())
				}
				if setting == "poll-failure" {
					if output.State != nil || output.StateError == "" {
						t.Fatalf("presented stale state after failed poll: %s", result.Output)
					}
				} else if setting == "off" {
					if output.State != nil {
						t.Fatalf("switch OFF changed payload: %s", result.Output)
					}
				} else if output.State == nil || len(output.State.TCPPose) != 6 || output.State.TCPPose[2] != 0.25 || output.State.Gripper != "open" {
					t.Fatalf("missing post-action state: %s", result.Output)
				}
			})
		}
	}
}
