package robotlab

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestVisionStep(t *testing.T) {
	for _, tc := range []struct {
		name     string
		answers  []string
		wantErr  bool
		verified bool
		acts     int
	}{
		{"good", []string{goodDecision, `{"verified":true}`}, false, true, 1},
		{"wrapped", []string{"```json\n" + goodDecision + "\n```", `{"verified":true}`}, false, true, 1},
		{"retry", []string{`oops`, goodDecision, `{"verified":true}`}, false, true, 1},
		{"unknown-fixed", []string{strings.ReplaceAll(goodDecision, `"mark":1`, `"mark":99`), goodDecision, `{"verified":true}`}, false, true, 1},
		{"unknown", []string{strings.ReplaceAll(goodDecision, `"mark":1`, `"mark":99`), strings.ReplaceAll(goodDecision, `"mark":1`, `"mark":99`)}, true, false, 0},
		{"malformed", []string{`{"objects":`, `garbage`}, true, false, 0},
		{"verify-false", []string{goodDecision, `{"verified":false}`}, false, false, 1},
		{"verify-retry", []string{goodDecision, `{"verified":"yes"}`, `{"verified":false}`}, false, false, 1},
		{"no-action", []string{`{"objects":[],"action":null,"explanation":"Nothing to move"}`}, false, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &toolSidecarFake{}
			p := &fakePublisher{}
			a := &fakeAsker{answers: tc.answers}
			loop := &VisionLoop{Sidecar: s, Publisher: p, Asker: a}
			d, e := loop.VisionStep(context.Background(), "s", "Sort red")
			if (e != nil) != tc.wantErr {
				t.Fatalf("%+v %v", d, e)
			}
			acts := 0
			for _, m := range s.calls {
				if m == "session.skill" {
					acts++
				}
			}
			if acts != tc.acts {
				t.Fatal(s.calls)
			}
			if !tc.wantErr && d.Action != nil && (d.Verified == nil || *d.Verified != tc.verified) {
				t.Fatal(d)
			}
			for _, prompt := range a.prompts {
				if strings.Contains(prompt, "ground-truth-secret") || strings.Contains(prompt, `"label"`) || strings.Contains(prompt, `"name"`) {
					t.Fatal("labels leaked", prompt)
				}
			}
			for _, payload := range p.payloads {
				if strings.Contains(payload, "ground-truth-secret") || strings.Contains(payload, `"label"`) {
					t.Fatal("labels leaked to event")
				}
			}
			if tc.wantErr && p.names[len(p.names)-1] != "robotlab.vision" {
				t.Fatal("missing error event")
			}
			if !tc.wantErr && acts > 0 {
				joined := strings.Join(p.payloads, " ")
				for _, phase := range []string{"see", "think", "act", "verify", "done"} {
					if !strings.Contains(joined, `"phase":"`+phase+`"`) {
						t.Fatal("missing phase", phase)
					}
				}
				if !strings.Contains(joined, "fake-vision") {
					t.Fatal("missing model")
				}
			}
			if len(a.prompts) > 2 && tc.acts == 0 && !strings.Contains(a.prompts[1], "previous answer") {
				t.Fatal("missing repair prompt")
			}
		})
	}
}
func TestVisionCancellationTimeoutUnavailable(t *testing.T) {
	for _, name := range []string{"timeout", "cancel", "cancel-between-phases", "unavailable", "skill-failure"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			s := &toolSidecarFake{}
			p := &fakePublisher{}
			a := &fakeAsker{answers: []string{goodDecision, `{"verified":true}`}}
			loop := &VisionLoop{Sidecar: s, Publisher: p, Asker: a}
			var expected error
			switch name {
			case "timeout":
				loop.Timeout = 5 * time.Millisecond
				a.call = func(c context.Context) (string, string, error) { <-c.Done(); return "", "", c.Err() }
				expected = context.DeadlineExceeded
			case "cancel":
				cancel()
				expected = context.Canceled
			case "cancel-between-phases":
				a.call = func(context.Context) (string, string, error) { cancel(); return goodDecision, "fake", nil }
				expected = context.Canceled
			case "unavailable":
				loop.Asker = nil
			case "skill-failure":
				s.call = func(_ context.Context, m string, _ any) (json.RawMessage, error) {
					if m == "session.snapshot" {
						return json.RawMessage(`{"image_png_b64":"png","marks":[{"id":1,"bbox":[0,0,1,1]}]}`), nil
					}
					return json.RawMessage(`{"ok":false,"detail":"blocked"}`), nil
				}
			}
			_, e := loop.VisionStep(ctx, "s", "sort")
			if e == nil {
				t.Fatal("expected failure")
			}
			if expected != nil && !errors.Is(e, expected) {
				t.Fatal(e)
			}
			if expected != nil || name == "unavailable" {
				for _, m := range s.calls {
					if m == "session.skill" {
						t.Fatal("acted after cancellation/unavailable")
					}
				}
			}
			payload := p.payloads[len(p.payloads)-1]
			if !strings.Contains(payload, `"phase":"error"`) {
				t.Fatal(payload)
			}
		})
	}
}
func TestVisionStrictJSON(t *testing.T) {
	for _, answer := range []string{`{}`, `{"objects":[],"explanation":"missing action"}`, `{"objects":[],"action":null,"explanation":"fine","label":"leak"}`, `{"objects":[],"action":null,"explanation":"fine","verified":true}`, strings.Repeat("x", 16385)} {
		_, e := parseVisionDecision(answer, []VisionMark{{ID: 1}})
		if e == nil {
			t.Fatal(answer)
		}
	}
}

func TestVisionClientHookSharesLoop(t *testing.T) {
	var hook func(context.Context, string, string) (any, error)
	s := &toolSidecarFake{}
	p := &fakePublisher{}
	asker := &fakeAsker{answers: []string{goodDecision, `{"verified":true}`}}
	tool := NewTool(s, p, asker, &hook)
	if hook == nil || tool.Vision == nil {
		t.Fatal("vision hook was not installed")
	}
	result, err := hook(context.Background(), "s", "sort red")
	if err != nil {
		t.Fatal(err)
	}
	decision, ok := result.(VisionDecision)
	if !ok || decision.Verified == nil || !*decision.Verified {
		t.Fatal(result)
	}
	if len(asker.prompts) != 2 {
		t.Fatal("client hook bypassed the shared loop")
	}
}

func TestVisionRejectsUnknownBinBeforeActing(t *testing.T) {
	bad := strings.ReplaceAll(goodDecision, `"target":"A"`, `"target":"red"`)
	s := &toolSidecarFake{}
	asker := &fakeAsker{answers: []string{bad, bad}}
	_, err := (&VisionLoop{Sidecar: s, Asker: asker}).VisionStep(context.Background(), "s", "sort")
	if err == nil || !strings.Contains(err.Error(), "target must be") {
		t.Fatal(err)
	}
	for _, method := range s.calls {
		if method == "session.skill" {
			t.Fatal("acted with an invalid bin")
		}
	}
}

func TestVisionSimulationVerification(t *testing.T) {
	for _, tc := range []struct {
		name, result       string
		disabled, verified bool
		note               string
	}{
		{"simulation-wins", `{"ok":true,"in_target":true,"bin":"A"}`, false, true, "simulation"},
		{"simulation-false", `{"ok":true,"in_target":false,"bin":"A"}`, false, false, "simulation"},
		{"real-robot", `{"ok":true}`, false, false, "model"},
		{"wrong-bin", `{"ok":true,"in_target":true,"bin":"B"}`, false, false, "model"},
		{"disabled", `{"ok":true,"in_target":true,"bin":"A"}`, true, false, "model"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.disabled {
				t.Setenv("EXCALIBUR_ROBOTLAB_VISION_GROUND_TRUTH", "off")
			}
			s := &toolSidecarFake{}
			snapshots := 0
			s.call = func(_ context.Context, method string, params any) (json.RawMessage, error) {
				if method == "session.skill" {
					return json.RawMessage(tc.result), nil
				}
				camera := params.(map[string]any)["camera"]
				if snapshots == 1 && (tc.note == "simulation" || tc.disabled) && camera != "bin_A" {
					t.Fatalf("verification camera: %v", camera)
				}
				snapshots++
				return json.RawMessage(`{"image_png_b64":"png","marks":[{"id":1,"bbox":[0,0,10,10]}]}`), nil
			}
			a := &fakeAsker{answers: []string{goodDecision, `{"verified":false}`}}
			d, err := (&VisionLoop{Sidecar: s, Asker: a}).VisionStep(context.Background(), "s", "Sort red")
			if err != nil || d.Verified == nil || *d.Verified != tc.verified || d.ModelVerified == nil || *d.ModelVerified || d.VerifyNote != tc.note {
				t.Fatalf("%+v %v", d, err)
			}
			if !strings.Contains(a.prompts[1], "mark 1") || !strings.Contains(a.prompts[1], `bin "A"`) {
				t.Fatal(a.prompts[1])
			}
		})
	}
}

func TestVisionPromptUsesInstructionLanguage(t *testing.T) {
	asker := &fakeAsker{}
	asker.call = func(_ context.Context) (string, string, error) {
		return `{"objects":[],"action":null,"explanation":"No hay piezas rojas"}`, "fake", nil
	}
	// fakeAsker records the prompt before invoking its callback.
	d, err := (&VisionLoop{Sidecar: &toolSidecarFake{}, Asker: asker}).VisionStep(context.Background(), "s", "Coloca las piezas rojas en A")
	if err != nil || d.Explanation != "No hay piezas rojas" {
		t.Fatalf("%+v %v", d, err)
	}
	prompt := asker.prompts[0]
	for _, required := range []string{"class, reason and explanation", "same language as the user's instruction", "Spanish instruction means Spanish text", "Coloca las piezas rojas en A"} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("prompt lacks %q", required)
		}
	}
}

func TestVisionInspectionCamera(t *testing.T) {
	for _, camera := range []string{"", "top"} {
		t.Run("camera-"+camera, func(t *testing.T) {
			t.Setenv("EXCALIBUR_ROBOTLAB_VISION_CAMERA", camera)
			want := camera
			if want == "" {
				want = "work"
			}
			s := &toolSidecarFake{}
			s.call = func(_ context.Context, method string, params any) (json.RawMessage, error) {
				p := params.(map[string]any)
				if method != "session.snapshot" || p["camera"] != want || p["width"] != 1024 || p["height"] != 768 {
					t.Fatalf("%s %+v", method, p)
				}
				return json.RawMessage(`{"image_png_b64":"png","marks":[]}`), nil
			}
			a := &fakeAsker{answers: []string{`{"objects":[],"action":null,"explanation":"Nothing to move"}`}}
			if _, err := (&VisionLoop{Sidecar: s, Asker: a}).VisionStep(context.Background(), "s", "sort"); err != nil {
				t.Fatal(err)
			}
		})
	}
}
