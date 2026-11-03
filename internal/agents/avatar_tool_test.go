package agents

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

func TestDesignAvatarTool(t *testing.T) {
	tool := &DesignAvatarTool{}
	if err := tool.Definition().Validate(); err != nil {
		t.Fatal(err)
	}
	spec := DefaultAvatar("Ada")
	spec.Variant, spec.Hair, spec.Accessory = "long-hair", "#8754b3", "glasses"
	for _, chosen := range []*AvatarSpec{nil, &spec} {
		raw, _ := json.Marshal(map[string]any{"description": "Girl with glasses and purple hair", "spec": chosen})
		result, err := tool.Invoke(context.Background(), tools.Invocation{Arguments: raw})
		if err != nil || result.Status != tools.StatusOK {
			t.Fatalf("design failed: %+v, %v", result, err)
		}
		var out struct {
			Spec    AvatarSpec `json:"spec"`
			Preview string     `json:"preview"`
		}
		if err := json.Unmarshal([]byte(result.Output), &out); err != nil {
			t.Fatal(err)
		}
		want := DefaultAvatar("Girl with glasses and purple hair")
		if chosen != nil {
			want, _ = ValidateAvatar(*chosen)
		}
		if out.Spec != want || !strings.Contains(out.Preview, want.Variant) || !strings.Contains(out.Preview, want.Accessory) || !strings.Contains(out.Preview, want.Hair) {
			t.Fatalf("spec or preview mismatch: %+v", out)
		}
	}
	for _, raw := range []string{
		`{}`, `null`, `{"description":" "}`, `{"description":3}`, `{"description":"x","extra":true}`,
		`{"description":"x","spec":{}}`, `{"description":"x","spec":{"variant":"robot"}}`,
		`{"description":"x"} {}`, `broken`,
		`{"description":"` + strings.Repeat("a", 2049) + `"}`,
		`{"description":"` + strings.Repeat("a", 17000) + `"}`,
	} {
		result, err := tool.Invoke(context.Background(), tools.Invocation{Arguments: json.RawMessage(raw)})
		if err != nil || result.Status != tools.StatusInputError {
			t.Fatalf("invalid input accepted: %+v, %v", result, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := tool.Invoke(ctx, tools.Invocation{Arguments: json.RawMessage(`{"description":"x"}`)})
	if err != nil || result.Status != tools.StatusCanceled {
		t.Fatalf("canceled call: %+v, %v", result, err)
	}
}
