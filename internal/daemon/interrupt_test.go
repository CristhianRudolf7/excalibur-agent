// Fast-lane control tests: 17.5 orders without a master turn or a
// model call, including with the provider down.
package daemon

import (
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestParseOrderForms(t *testing.T) {
	cases := []struct {
		text   string
		kind   OrderKind
		target string
	}{
		{"/stop", OrderStopTurn, ""},
		{"para", OrderStopTurn, ""},
		{"/stop all", OrderPauseAll, ""},
		{"para todo", OrderPauseAll, ""},
		{"para T12", OrderPause, "t12"},
		{"/pause run_abc", OrderPause, "run_abc"},
		{"detén run_abc", OrderPause, "run_abc"},
		{"cancela T12", OrderCancel, "t12"},
		{"/cancel run_abc", OrderCancel, "run_abc"},
		{"descarta T12", OrderCancel, "t12"},
		{"sigue T12", OrderResume, "t12"},
		{"/resume run_abc", OrderResume, "run_abc"},
	}
	for _, c := range cases {
		o, err := ParseOrder(c.text)
		if err != nil {
			t.Errorf("ParseOrder(%q): %v", c.text, err)
			continue
		}
		if o.Kind != c.kind || o.Target != c.target {
			t.Errorf("ParseOrder(%q) = (%v,%q), want (%v,%q)", c.text, o.Kind, o.Target, c.kind, c.target)
		}
		if o.Raw == "" {
			t.Errorf("ParseOrder(%q) must keep the raw text", c.text)
		}
	}
}

func TestParseOrderRejects(t *testing.T) {
	for _, text := range []string{"", "  ", "hola", "/pause", "cancela", "sigue", "para todo ya"} {
		if _, err := ParseOrder(text); err == nil {
			t.Errorf("ParseOrder(%q) must fail, never guess a target", text)
		}
	}
}

// fastControl records orders with zero model calls by construction:
// it has no model handle.
type fastControl struct {
	turns    []string
	pauses   []string
	cancels  []string
	all      int
	resumes  []string
	provider bool // true simulates the provider down
}

func (f *fastControl) InterruptTurn(conv string) error {
	f.turns = append(f.turns, conv)
	return nil
}

func (f *fastControl) PauseRun(target string, _ types.PauseReason) error {
	f.pauses = append(f.pauses, target)
	return nil
}

func (f *fastControl) CancelRun(target string) error {
	f.cancels = append(f.cancels, target)
	return nil
}

func (f *fastControl) PauseAll() error {
	f.all++
	return nil
}

func (f *fastControl) ResumeRun(target string) error {
	f.resumes = append(f.resumes, target)
	return nil
}

func TestApplyFastLaneWithProviderDown(t *testing.T) {
	ctl := &fastControl{provider: true} // provider down: lane must still work
	for text, want := range map[string]func() int{
		"/stop":        func() int { return len(ctl.turns) },
		"para T1":      func() int { return len(ctl.pauses) },
		"cancela T1":   func() int { return len(ctl.cancels) },
		"para todo":    func() int { return ctl.all },
		"sigue T1":     func() int { return len(ctl.resumes) },
		"/pause run_x": func() int { return len(ctl.pauses) },
	} {
		o, err := ParseOrder(text)
		if err != nil {
			t.Fatalf("ParseOrder(%q): %v", text, err)
		}
		before := want()
		if err := Apply(o, "cnv_main", ctl); err != nil {
			t.Fatalf("Apply(%q) with provider down: %v", text, err)
		}
		if want() != before+1 {
			t.Errorf("Apply(%q) did not dispatch", text)
		}
	}
	if len(ctl.turns) != 1 || ctl.turns[0] != "cnv_main" {
		t.Errorf("stop without target hits the current conversation: %v", ctl.turns)
	}
}

func TestStopTurnLeavesBackgroundAlone(t *testing.T) {
	ctl := &fastControl{}
	o, _ := ParseOrder("/stop")
	if err := Apply(o, "cnv_a", ctl); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(ctl.pauses) != 0 || ctl.all != 0 {
		t.Errorf("stop turn must not pause background work: %+v", ctl)
	}
}

func TestOrderKindStrings(t *testing.T) {
	for k, want := range map[OrderKind]string{
		OrderStopTurn: "stop_turn", OrderPause: "pause", OrderCancel: "cancel",
		OrderPauseAll: "pause_all", OrderResume: "resume",
	} {
		if k.String() != want {
			t.Errorf("kind %d = %q, want %q", int(k), k.String(), want)
		}
	}
	if !strings.Contains(OrderPauseAll.String(), "pause") {
		t.Error("unreachable")
	}
}
