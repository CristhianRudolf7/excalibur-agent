package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallPrintAllClients(t *testing.T) {
	for _, client := range []string{ClientCodex, ClientClaudeCode, ClientOpenCode, ClientMuseCode} {
		var buf bytes.Buffer
		if err := InstallMCP(client, true, false, &buf); err != nil {
			t.Fatalf("client %s: %v", client, err)
		}
		if buf.Len() == 0 {
			t.Fatalf("client %s: empty fragment", client)
		}
		body := buf.String()
		if !strings.Contains(body, "excalibur") || !strings.Contains(body, `"mcp"`) && !strings.Contains(body, "mcp_servers") && !strings.Contains(body, "mcpServers") && !strings.Contains(body, `"mcp"`) {
			t.Fatalf("client %s fragment has no MCP hookup: %q", client, body)
		}
		// The --print fragment and the shipped template agree on the
		// hookup: same server name, same command, same transport
		// shape (Section 3.6: formats verified per client at
		// implementation; comments may differ).
		template := filepath.Join("..", "..", "integrations", "clients", clientFile(client))
		raw, err := os.ReadFile(template)
		if err != nil {
			t.Fatalf("client %s template: %v", client, err)
		}
		for _, marker := range []string{"excalibur", "mcp"} {
			if !strings.Contains(string(raw), marker) {
				t.Fatalf("client %s template misses %q", client, marker)
			}
		}
		var parsed any
		if strings.HasSuffix(clientFile(client), ".json") {
			if err := json.Unmarshal(raw, &parsed); err != nil {
				t.Fatalf("client %s template is not JSON: %v", client, err)
			}
		}
	}
	if err := InstallMCP("nope", true, false, &bytes.Buffer{}); err == nil {
		t.Fatal("unknown client must fail")
	}
}

func clientFile(client string) string {
	switch client {
	case ClientCodex:
		return "codex.toml"
	case ClientClaudeCode:
		return "claude-code.json"
	case ClientOpenCode:
		return "opencode.json"
	case ClientMuseCode:
		return "muse-code.json"
	default:
		return ""
	}
}

func TestInstallPrintValidates(t *testing.T) {
	// Every --print fragment must be valid for its client format:
	// JSON parses, the TOML names the excalibur server section.
	var buf bytes.Buffer
	if err := InstallMCP(ClientOpenCode, true, false, &buf); err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(buf.Bytes(), &cfg); err != nil {
		t.Fatalf("opencode fragment is not JSON: %v", err)
	}
	mcp, _ := cfg["mcp"].(map[string]any)
	entry, _ := mcp["excalibur"].(map[string]any)
	if entry["type"] != "local" {
		t.Fatalf("opencode entry = %v", entry)
	}
	buf.Reset()
	if err := InstallMCP(ClientClaudeCode, true, false, &buf); err != nil {
		t.Fatal(err)
	}
	var claude map[string]any
	if err := json.Unmarshal(buf.Bytes(), &claude); err != nil {
		t.Fatalf("claude fragment is not JSON: %v", err)
	}
	buf.Reset()
	if err := InstallMCP(ClientCodex, true, false, &buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "[mcp_servers.excalibur]") {
		t.Fatalf("codex fragment: %q", buf.String())
	}
}

func TestInstallWritesAndSkill(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	var buf bytes.Buffer
	if err := InstallMCP(ClientOpenCode, false, true, &buf); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(home, ".config", "opencode", "opencode.json")
	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("written config: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	// Installing twice merges instead of duplicating.
	var buf2 bytes.Buffer
	if err := InstallMCP(ClientOpenCode, false, false, &buf2); err != nil {
		t.Fatal(err)
	}
	raw2, _ := os.ReadFile(target)
	var cfg2 map[string]any
	if err := json.Unmarshal(raw2, &cfg2); err != nil {
		t.Fatal(err)
	}
	mcp, _ := cfg2["mcp"].(map[string]any)
	if len(mcp) != 1 {
		t.Fatalf("merge duplicated: %v", mcp)
	}
	// The skill copy matches the shipped SKILL.md.
	skillDest := filepath.Join(home, ".config", "opencode", "skills", "excalibur-delegate", "SKILL.md")
	installed, err := os.ReadFile(skillDest)
	if err != nil {
		t.Fatalf("skill: %v", err)
	}
	shipped, err := os.ReadFile(filepath.Join("..", "..", "integrations", "clients", "excalibur-delegate", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(installed) != string(shipped) {
		t.Fatal("installed skill drifted from integrations/clients/excalibur-delegate/SKILL.md")
	}
	if string(installed) != delegateSkill() {
		t.Fatal("embedded skill drifted from the shipped SKILL.md")
	}
	// An existing config without excalibur keeps its entries.
	custom := filepath.Join(home, ".config", "opencode", "custom.json")
	_ = custom
	pre := `{"mcp": {"other": {"type": "local", "command": ["other"], "enabled": true}}}`
	if err := os.WriteFile(target, []byte(pre), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf3 bytes.Buffer
	if err := InstallMCP(ClientOpenCode, false, false, &buf3); err != nil {
		t.Fatal(err)
	}
	raw3, _ := os.ReadFile(target)
	var cfg3 map[string]any
	if err := json.Unmarshal(raw3, &cfg3); err != nil {
		t.Fatal(err)
	}
	mcp3, _ := cfg3["mcp"].(map[string]any)
	if mcp3["other"] == nil || mcp3["excalibur"] == nil {
		t.Fatalf("merge lost entries: %v", mcp3)
	}
}
