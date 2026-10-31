# Client integrations

Per-client configuration fragments (`excalibur mcp install --print`)
and the `excalibur-delegate` Agent Skill for agents without MCP
(Section 3.6). Implemented in milestone H4 closure + H8.

## Fragments

| Client | Fragment | Target |
|---|---|---|
| Codex CLI | `codex.toml` | `~/.codex/config.toml` (`[mcp_servers.excalibur]`) |
| Claude Code | `claude-code.json` | `~/.claude.json` (`mcpServers.excalibur`); or `claude mcp add excalibur -- excalibur mcp` |
| OpenCode | `opencode.json` | `~/.config/opencode/opencode.json` (`mcp.excalibur`, type `local`) |
| Muse Code | `muse-code.json` | `~/.muse/mcp.json` (`mcpServers.excalibur`) |

Each fragment records the client version checked at implementation
time in its header comment. Install with:

```sh
excalibur mcp install --client <codex|claude-code|opencode|muse-code> [--print] [--skill]
```

`--print` writes the fragment to stdout instead of the client file;
`--skill` also copies `excalibur-delegate/SKILL.md` to the client's
skill directory.

## Delegate skill

`excalibur-delegate/SKILL.md` is the Agent Skills skill for agents
without MCP. It explains when to delegate and how to drive
`excalibur task` step by step. The installed copy and the
`internal/cli` embedded copy are pinned equal by backend tests so
the two never drift.
