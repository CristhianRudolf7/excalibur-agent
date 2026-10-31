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

func TestManifestValidate(t *testing.T) {
	if err := sampleManifest().Validate(); err != nil {
		t.Fatalf("sample must validate: %v", err)
	}
	bad := sampleManifest()
	bad.Contract = "excalibur-ext/9"
	if err := bad.Validate(); err == nil {
		t.Fatal("unsupported contract must fail")
	}
	empty := sampleManifest()
	empty.Tools = nil
	empty.Kind = ExtensionBuiltin
	if err := empty.Validate(); err == nil {
		t.Fatal("builtin without tools must fail")
	}
	mcp := sampleManifest()
	mcp.Tools = nil
	mcp.Kind = ExtensionMCP
	if err := mcp.Validate(); err != nil {
		t.Fatalf("mcp may declare no static tools: %v", err)
	}
	noDry := sampleManifest()
	noDry.Tools[0].Effect = EffectExec
	noDry.Tools[0].Idempotency = IdemKeyed
	if err := noDry.Validate(); err != nil {
		t.Fatalf("validate does not gate dry_run (the conformance suite does): %v", err)
	}
}

func TestManifestYAMLRoundtrip(t *testing.T) {
	in := sampleManifest()
	in.Tools[0].Timeout = 90 * 1_000_000_000
	raw, err := MarshalManifestYAML(in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := UnmarshalManifestYAML(raw)
	if err != nil {
		t.Fatalf("yaml roundtrip: %v\n%s", err, raw)
	}
	if err := out.Validate(); err != nil {
		t.Fatalf("yaml result must validate: %v", err)
	}
	if out.Tools[0].Name != "demo_run" || out.Tools[0].Timeout != 90*1_000_000_000 {
		t.Fatalf("yaml lost fields: %+v", out.Tools[0])
	}
	var schemas map[string]any
	if err := json.Unmarshal(out.Tools[0].InputSchema, &schemas); err != nil {
		t.Fatalf("schema must stay JSON: %v", err)
	}
}

func TestManifestYAMLShapes(t *testing.T) {
	raw := []byte("id: x\nversion: '1'\ncontract: excalibur-ext/1\nkind: builtin\n" +
		"tools:\n- name: t_run\n  description: Run it\n" +
		"  input_schema:\n    type: object\n    properties:\n      q:\n        type: string\n" +
		"  effect: read_only\n  idempotency: pure\n  cancel: immediate\n  timeout: 30s\n")
	m, err := UnmarshalManifestYAML(raw)
	if err != nil {
		t.Fatalf("map schemas and duration strings: %v", err)
	}
	if string(m.Tools[0].InputSchema) == "" || m.Tools[0].Timeout != 30*1_000_000_000 {
		t.Fatalf("yaml shapes misread: %+v", m.Tools[0])
	}
	nanos := []byte("id: x\nversion: '1'\ncontract: excalibur-ext/1\nkind: builtin\n" +
		"tools:\n- name: t_run\n  description: Run it\n  input_schema: '{\"type\":\"object\"}'\n" +
		"  effect: exec\n  idempotency: keyed\n  cancel: graceful\n  timeout: 60000000000\n  dry_run: none\n")
	m2, err := UnmarshalManifestYAML(nanos)
	if err != nil {
		t.Fatalf("inline JSON schemas and nanos timeouts: %v", err)
	}
	if m2.Tools[0].Timeout != 60*1_000_000_000 {
		t.Fatalf("nanos timeout misread: %v", m2.Tools[0].Timeout)
	}
}
