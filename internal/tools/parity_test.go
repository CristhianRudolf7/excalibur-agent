package tools

import (
	"encoding/json"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/pkg/extsdk"
)

// TestExtSDKManifestParity asserts the public SDK manifest and the
// internal manifest stay convertible via JSON, so a manifest written
// against either validates on both sides.
func TestExtSDKManifestParity(t *testing.T) {
	sdk := extsdk.Manifest{
		ID:       "com.example.demo",
		Version:  "1.0.0",
		Contract: extsdk.ExtensionContract,
		Kind:     extsdk.ExtensionPackage,
		Tools: []extsdk.ExtensionTool{
			{Definition: extsdk.Definition{
				Name:        "demo_run",
				Description: "Run a demo operation",
				InputSchema: json.RawMessage(`{"type":"object"}`),
				Effect:      extsdk.EffectExec,
				Idempotency: extsdk.IdemKeyed,
				Cancel:      extsdk.CancelGraceful,
			}},
		},
		Lifecycle: extsdk.LifecycleLazy,
	}
	raw, err := json.Marshal(sdk)
	if err != nil {
		t.Fatal(err)
	}
	var internal Manifest
	if err := json.Unmarshal(raw, &internal); err != nil {
		t.Fatal(err)
	}
	if err := internal.Validate(); err != nil {
		t.Fatalf("sdk manifest must validate internally: %v", err)
	}
	if internal.Tools[0].Name != "demo_run" || internal.Tools[0].Effect != "exec" {
		t.Fatalf("parity lost fields: %+v", internal.Tools[0])
	}
	// And back: internal -> SDK JSON shape.
	raw2, err := json.Marshal(internal)
	if err != nil {
		t.Fatal(err)
	}
	var back extsdk.Manifest
	if err := json.Unmarshal(raw2, &back); err != nil {
		t.Fatal(err)
	}
	if back.ID != sdk.ID || back.Contract != sdk.Contract || len(back.Tools) != 1 {
		t.Fatalf("internal manifest must convert back: %+v", back)
	}
}

func TestExtSDKResultImageParity(t *testing.T) {
	original := extsdk.Result{Status: "ok", Output: "state", Images: []extsdk.ImageInput{{URL: "data:image/png;base64,aA=="}}}
	raw, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var internal Result
	if err = json.Unmarshal(raw, &internal); err != nil || len(internal.Images) != 1 || internal.Images[0].URL != original.Images[0].URL {
		t.Fatalf("%+v %v", internal, err)
	}
	raw, err = json.Marshal(internal)
	if err != nil {
		t.Fatal(err)
	}
	var back extsdk.Result
	if err = json.Unmarshal(raw, &back); err != nil || len(back.Images) != 1 || back.Images[0].URL != original.Images[0].URL {
		t.Fatalf("%+v %v", back, err)
	}
}
