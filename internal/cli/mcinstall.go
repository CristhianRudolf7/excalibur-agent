package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Supported MCP client names for `mcp install --client`.
const (
	ClientCodex      = "codex"
	ClientClaudeCode = "claude-code"
	ClientOpenCode   = "opencode"
	ClientMuseCode   = "muse-code"
)

// ClientFragment is one generated MCP client configuration.
type ClientFragment struct {
	Client string
	Path   string
	Body   string
}

// ClientFragmentFor builds the configuration fragment for a client.
// The templates live in integrations/clients/; the fragment records
// the client version checked at implementation time (Section 3.6:
// formats are verified when implemented).
func ClientFragmentFor(client string) (*ClientFragment, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("cli: home directory: %w", err)
	}
	switch client {
	case ClientCodex:
		return &ClientFragment{
			Client: client,
			Path:   filepath.Join(home, ".codex", "config.toml"),
			Body: "[mcp_servers.excalibur]\n" +
				"# Managed by `excalibur mcp install --client codex`.\n" +
				"# Checked against Codex CLI config format as of 2026-10.\n" +
				"command = \"excalibur\"\n" +
				"args = [\"mcp\"]\n",
		}, nil
	case ClientClaudeCode:
		return &ClientFragment{
			Client: client,
			Path:   filepath.Join(home, ".claude.json"),
			Body: "{\n  \"$comment\": \"Managed by `excalibur mcp install --client claude-code`. " +
				"Checked against the Claude Code MCP format as of 2026-10. " +
				"Equivalent command: claude mcp add excalibur -- excalibur mcp.\",\n" +
				"  \"mcpServers\": {\n    \"excalibur\": {\n" +
				"      \"command\": \"excalibur\",\n      \"args\": [\"mcp\"]\n    }\n  }\n}\n",
		}, nil
	case ClientOpenCode:
		return &ClientFragment{
			Client: client,
			Path:   filepath.Join(home, ".config", "opencode", "opencode.json"),
			Body: "{\n  \"$schema\": \"https://opencode.ai/config.json\",\n" +
				"  \"mcp\": {\n    \"excalibur\": {\n" +
				"      \"type\": \"local\",\n" +
				"      \"command\": [\"excalibur\", \"mcp\"],\n" +
				"      \"enabled\": true\n    }\n  }\n}\n",
		}, nil
	case ClientMuseCode:
		return &ClientFragment{
			Client: client,
			Path:   filepath.Join(home, ".muse", "mcp.json"),
			Body: "{\n  \"$comment\": \"Managed by `excalibur mcp install --client muse-code`. " +
				"Checked against the Muse Code MCP format as of 2026-10.\",\n" +
				"  \"mcpServers\": {\n    \"excalibur\": {\n" +
				"      \"command\": \"excalibur\",\n      \"args\": [\"mcp\"]\n    }\n  }\n}\n",
		}, nil
	default:
		return nil, fmt.Errorf("cli: unknown client %q (want codex|claude-code|opencode|muse-code)", client)
	}
}

// InstallMCP writes the client configuration fragment, or prints it
// with printOnly. With withSkill it also copies the
// excalibur-delegate Agent Skill to the client's skill directory for
// agents without MCP (Section 3.6).
func InstallMCP(client string, printOnly, withSkill bool, stdout io.Writer) error {
	frag, err := ClientFragmentFor(client)
	if err != nil {
		return &CodedError{Code: ExitError, Err: err}
	}
	if printOnly {
		if _, err := fmt.Fprint(stdout, frag.Body); err != nil {
			return &CodedError{Code: ExitError, Err: err}
		}
	} else {
		if err := writeFragment(frag); err != nil {
			return &CodedError{Code: ExitError, Err: err}
		}
		if _, err := fmt.Fprintf(stdout, "wrote %s\n", frag.Path); err != nil {
			return &CodedError{Code: ExitError, Err: err}
		}
	}
	if withSkill {
		dest, err := InstallDelegateSkill(client)
		if err != nil {
			return &CodedError{Code: ExitError, Err: err}
		}
		if _, err := fmt.Fprintf(stdout, "installed skill at %s\n", dest); err != nil {
			return &CodedError{Code: ExitError, Err: err}
		}
	}
	return nil
}

func writeFragment(frag *ClientFragment) error {
	existing, err := os.ReadFile(frag.Path)
	if err == nil && strings.Contains(string(existing), "excalibur") {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(frag.Path), 0o755); err != nil {
		return fmt.Errorf("cli: mkdir client config: %w", err)
	}
	merged := frag.Body
	if err == nil && frag.Client == ClientOpenCode {
		mergedBody, mergeErr := mergeOpenCodeConfig(existing, frag.Body)
		if mergeErr == nil {
			merged = mergedBody
		}
	}
	if err := os.WriteFile(frag.Path, []byte(merged), 0o644); err != nil {
		return fmt.Errorf("cli: write client config: %w", err)
	}
	return nil
}

func mergeOpenCodeConfig(existing []byte, _ string) (string, error) {
	var cfg map[string]any
	if err := json.Unmarshal(existing, &cfg); err != nil {
		return "", err
	}
	mcp, _ := cfg["mcp"].(map[string]any)
	if mcp == nil {
		mcp = map[string]any{}
		cfg["mcp"] = mcp
	}
	mcp["excalibur"] = map[string]any{
		"type": "local", "command": []string{"excalibur", "mcp"}, "enabled": true,
	}
	buf, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	return string(append(buf, '\n')), nil
}

// InstallDelegateSkill copies the excalibur-delegate skill to the
// client's skill directory.
func InstallDelegateSkill(client string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cli: home directory: %w", err)
	}
	var dir string
	switch client {
	case ClientCodex:
		dir = filepath.Join(home, ".codex", "skills", "excalibur-delegate")
	case ClientClaudeCode:
		dir = filepath.Join(home, ".claude", "skills", "excalibur-delegate")
	case ClientOpenCode:
		dir = filepath.Join(home, ".config", "opencode", "skills", "excalibur-delegate")
	case ClientMuseCode:
		dir = filepath.Join(home, ".muse", "skills", "excalibur-delegate")
	default:
		return "", fmt.Errorf("cli: unknown client %q", client)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("cli: mkdir skill dir: %w", err)
	}
	dest := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(dest, []byte(delegateSkill()), 0o644); err != nil {
		return "", fmt.Errorf("cli: write skill: %w", err)
	}
	return dest, nil
}

// delegateSkill returns the excalibur-delegate Agent Skill body. The
// same text ships in integrations/clients/excalibur-delegate/SKILL.md;
// the backend test pins them equal so the two copies never drift.
func delegateSkill() string {
	return `---
name: excalibur-delegate
description: Delegate a self-contained task to the Excalibur agent harness via its CLI or MCP server, then follow progress and collect the delivery report.
license: Excalibur Source-Available License
---

# Excalibur delegation

Use this skill when a task is self-contained and verifiable (implement a
function with its tests, fix a reproducible bug, investigate a repo,
migrate a module) or when long work should leave your own context free.
Do not delegate one-line changes or decisions that depend on the current
conversation.

## Delegate over the CLI

Create the task and return immediately:

    excalibur task start "<request>" --project DIR [--profile delegate] [--worktree] [--budget-usd X] [--max-steps N] [--timeout S]

Output is JSON: {"task_id": "T45", "status": "in_progress"}.

Follow progress and collect the result:

    excalibur task status T45
    excalibur task wait T45 --timeout 300
    excalibur task result T45 [--format md]

If the task asks a question, answer it or add a directive:

    excalibur task reply T45 "<text>"

Control it with pause, resume, cancel; inspect with list and logs:

    excalibur task pause T45
    excalibur task resume T45
    excalibur task cancel T45
    excalibur task list [--project DIR]
    excalibur task logs T45 [--follow]

Exit codes match the run state: 0 completed, 2 completed with pending
items, 3 blocked, 4 paused, 5 failed, 130 canceled, 1 harness error.

With --never-ask the task assumes instead of asking and records every
assumption in the delivery report.

## Delegate over MCP

When the client supports MCP, configure the excalibur server with:

    excalibur mcp install --client <codex|claude-code|opencode|muse-code> [--print]

Then call excalibur_delegate (set wait:true for the final result),
excalibur_status, excalibur_wait, excalibur_result, excalibur_reply,
excalibur_cancel and excalibur_list. Progress arrives as MCP progress
notifications during wait.

## Results

Results name the branch and diff when a worktree was used; nothing in
the delegating tree changes until you integrate it. The report carries
the summary, files changed, criteria with evidence, assumptions and
anything still pending.
`
}
