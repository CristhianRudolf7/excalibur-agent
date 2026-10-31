package vault

import (
	"context"
	"encoding/json"
	"fmt"
)

// Secrets tool operations (the agent-facing `secrets` tool of Section
// 20.1). The model sees names, types, scopes and uses, never values.
// Value-bearing ops (create/update/get/reveal) are NOT tool ops: the
// value is entered by the user through the CLI terminal or the secure
// client form, never through chat or the model.
const (
	SecretsOpList          = "list"
	SecretsOpDescribe      = "describe"
	SecretsOpGenerate      = "generate"
	SecretsOpRequest       = "request"
	SecretsOpRequestChange = "request_change"
	SecretsOpRequestDelete = "request_delete"
	SecretsOpUsage         = "usage"
	SecretsOpConfirmDraft  = "confirm_draft"
	SecretsOpDiscardDraft  = "discard_draft"
	SecretsOpListDrafts    = "list_drafts"
	SecretsOpRedact        = "redact"
	maxSecretsToolList     = 500
)

// SecretsToolManifest describes the `secrets` tool for catalog
// registration (kept here so tools-package wiring is declarative).
// No schema ever carries a value field.
func SecretsToolManifest() map[string]any {
	return map[string]any{
		"name":        "secrets",
		"description": "Manage secret references: list names, types, scopes and uses; request or generate values. Never returns secret values; use secret://name references in tool arguments and the executor resolves them in-process.",
		"effect":      "write_local",
		"idempotency": "keyed",
		"group":       "security",
		"operations": []string{
			SecretsOpList, SecretsOpDescribe, SecretsOpGenerate,
			SecretsOpRequest, SecretsOpRequestChange, SecretsOpRequestDelete,
			SecretsOpUsage, SecretsOpConfirmDraft, SecretsOpDiscardDraft,
			SecretsOpListDrafts, SecretsOpRedact,
		},
	}
}

// PendingRequest is a value action the agent may NOT complete itself:
// the client must show the user a secure form (or a confirmation) and
// call back into the vault API. This is how `request`, `request_change`
// and `request_delete` stay value-free.
type PendingRequest struct {
	Kind      string         `json:"kind"`
	Secret    string         `json:"secret"`
	Question  string         `json:"question"`
	Blocking  bool           `json:"blocking"`
	FormHints map[string]any `json:"form_hints,omitempty"`
}

// ToolResult is the value-free outcome of a secrets-tool call.
type ToolResult struct {
	Output       string          `json:"output"`
	Entries      []Metadata      `json:"entries,omitempty"`
	Uses         []UseRecord     `json:"uses,omitempty"`
	Drafts       []DraftInfo     `json:"drafts,omitempty"`
	NeedsUser    *PendingRequest `json:"needs_user,omitempty"`
	RedactedText *string         `json:"redacted_text,omitempty"`
}

// SecretsArgs carries one secrets-tool invocation.
type SecretsArgs struct {
	Op           string
	Name         string
	Type         string
	Length       int
	Charset      string
	Meta         map[string]string
	Scopes       []string
	AllowedTools []string
	Domains      []string
	DraftID      string
	Text         string
	Limit        int
}

// Dispatch runs one secrets-tool operation. It never returns, logs or
// leaks a secret value: results carry metadata, markers and pending
// user questions only.
func (v *Vault) Dispatch(ctx context.Context, args SecretsArgs) (ToolResult, error) {
	switch args.Op {
	case SecretsOpList:
		entries, err := v.List(ctx)
		if err != nil {
			return ToolResult{}, err
		}
		if len(entries) > maxSecretsToolList {
			entries = entries[:maxSecretsToolList]
		}
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name)
		}
		return ToolResult{Output: fmt.Sprintf("%d secrets: %v", len(entries), names), Entries: entries}, nil

	case SecretsOpDescribe:
		md, err := v.Describe(ctx, args.Name)
		if err != nil {
			return ToolResult{}, err
		}
		return ToolResult{Output: fmt.Sprintf("%s (%s)", md.Name, md.Type), Entries: []Metadata{md}}, nil

	case SecretsOpGenerate:
		typ := args.Type
		if typ == "" {
			typ = TypeToken
		}
		md, err := v.GenerateAndStore(ctx, GenerateParams{
			Name: args.Name, Type: typ, Length: args.Length, Charset: args.Charset,
			Meta: args.Meta, Scopes: args.Scopes, AllowedTools: args.AllowedTools, Domains: args.Domains,
		})
		if err != nil {
			return ToolResult{}, err
		}
		return ToolResult{
			Output:  fmt.Sprintf("generated %s (%s); the value is stored and never shown", md.Name, md.Type),
			Entries: []Metadata{md},
		}, nil

	case SecretsOpRequest:
		if err := validateName(args.Name); err != nil {
			return ToolResult{}, err
		}
		typ := args.Type
		if typ == "" {
			typ = TypeNote
		}
		if !ValidType(typ) {
			return ToolResult{}, fmt.Errorf("%w: %q", ErrBadType, typ)
		}
		return ToolResult{
			Output: fmt.Sprintf("ask the user to enter the value for %s via the secure form", args.Name),
			NeedsUser: &PendingRequest{
				Kind:     "vault_set",
				Secret:   args.Name,
				Question: fmt.Sprintf("Enter the value for secret %q (type %s). Input is never sent through chat.", args.Name, typ),
				Blocking: true,
				FormHints: map[string]any{
					"type": args.Type, "metadata": args.Meta, "scopes": args.Scopes,
					"allowed_tools": args.AllowedTools, "domains": args.Domains,
				},
			},
		}, nil

	case SecretsOpRequestChange:
		if _, err := v.Describe(ctx, args.Name); err != nil {
			return ToolResult{}, err
		}
		return ToolResult{
			Output: fmt.Sprintf("ask the user to confirm the policy change for %s", args.Name),
			NeedsUser: &PendingRequest{
				Kind:     "vault_edit",
				Secret:   args.Name,
				Question: fmt.Sprintf("Confirm changing scopes/tools/domains for secret %q.", args.Name),
				Blocking: true,
				FormHints: map[string]any{
					"scopes": args.Scopes, "allowed_tools": args.AllowedTools, "domains": args.Domains,
				},
			},
		}, nil

	case SecretsOpRequestDelete:
		if _, err := v.Describe(ctx, args.Name); err != nil {
			return ToolResult{}, err
		}
		return ToolResult{
			Output: fmt.Sprintf("ask the user to confirm deleting %s (irreversible)", args.Name),
			NeedsUser: &PendingRequest{
				Kind:     "vault_delete",
				Secret:   args.Name,
				Question: fmt.Sprintf("Confirm deleting secret %q. This is irreversible.", args.Name),
				Blocking: true,
			},
		}, nil

	case SecretsOpUsage:
		uses, err := v.Usage(ctx, args.Name, args.Limit)
		if err != nil {
			return ToolResult{}, err
		}
		return ToolResult{Output: fmt.Sprintf("%s used %d times", args.Name, len(uses)), Uses: uses}, nil

	case SecretsOpConfirmDraft:
		md, err := v.ConfirmDraft(ctx, args.DraftID, SetParams{
			Name: args.Name, Type: args.Type, Meta: args.Meta,
			Scopes: args.Scopes, AllowedTools: args.AllowedTools, Domains: args.Domains,
		})
		if err != nil {
			return ToolResult{}, err
		}
		return ToolResult{Output: fmt.Sprintf("draft stored as %s", md.Name), Entries: []Metadata{md}}, nil

	case SecretsOpDiscardDraft:
		if err := v.DiscardDraft(ctx, args.DraftID); err != nil {
			return ToolResult{}, err
		}
		return ToolResult{Output: "draft discarded"}, nil

	case SecretsOpListDrafts:
		drafts, err := v.ListDrafts(ctx)
		if err != nil {
			return ToolResult{}, err
		}
		return ToolResult{Output: fmt.Sprintf("%d pending drafts", len(drafts)), Drafts: drafts}, nil

	case SecretsOpRedact:
		red := v.Redact(args.Text)
		return ToolResult{Output: "redacted", RedactedText: &red}, nil

	default:
		return ToolResult{}, fmt.Errorf("vault: unknown secrets op %q", args.Op)
	}
}

// MarshalResult renders a ToolResult as JSON for the engine. Values
// cannot appear: Dispatch never puts them in.
func MarshalResult(r ToolResult) string {
	raw, err := json.Marshal(r)
	if err != nil {
		return `{"output":"vault: encode error"}`
	}
	return string(raw)
}
