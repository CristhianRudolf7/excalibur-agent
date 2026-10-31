package extsdk

import (
	"encoding/json"
	"testing"
)

// sampleManifest builds a manifest exercising every field.
func sampleManifest() Manifest {
	return Manifest{
		ID:        "com.example.demo",
		Version:   "1.0.0",
		Contract:  ExtensionContract,
		Kind:      ExtensionPackage,
		Transport: "process",
		Command:   []string{"demo-tool"},
		Tools: []ExtensionTool{
			{Definition: Definition{
				Name:        "demo_run",
				Description: "Run a demo operation on a target",
				InputSchema: json.RawMessage(`{"type":"object"}`),
				Effect:      EffectExec,
				Idempotency: IdemKeyed,
				Cancel:      CancelGraceful,
				Fast:        true,
				Group:       "demo",
				Tags:        []string{"demo"},
				Keywords:    []string{"run", "demo", "target"},
			}},
		},
		Platforms:   []string{"linux"},
		Profiles:    []string{"default", "delegate"},
		Resources:   &ResourceLimits{RAMBytes: 1 << 20, MaxCalls: 4},
		Concurrency: 2,
		Lifecycle:   LifecycleLazy,
		HealthCheck: "demo ping",
	}
}

func TestManifestJSONRoundtrip(t *testing.T) {
	raw, err := json.Marshal(sampleManifest())
	if err != nil {
		t.Fatal(err)
	}
	var decoded Manifest
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.ID != "com.example.demo" || len(decoded.Tools) != 1 ||
		decoded.Tools[0].Name != "demo_run" {
		t.Fatalf("roundtrip lost data: %+v", decoded)
	}
}
