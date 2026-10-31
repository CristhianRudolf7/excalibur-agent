package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"

	"github.com/CristhianRudolf7/excalibur-agent/internal/capabilities"
)

// CapabilityInvoker runs one capability operation. It mirrors
// capabilities.Capability so tests can inject fakes without importing
// the full daemon wiring.
type CapabilityInvoker interface {
	Manifest() capabilities.Manifest
	Invoke(operation string, args json.RawMessage, idempotencyKey string) (capabilities.TaskResult, error)
	Poll(taskID string) (capabilities.TaskResult, error)
	Cancel(taskID string) (capabilities.TaskResult, error)
	Update(taskID string, answer json.RawMessage) (capabilities.TaskResult, error)
}

// ToolsFromCapability converts every operation of a black-box
// capability into a deferred tool (Section 14.1: the harness exposes
// each operation as a deferred catalog entry). The returned tools
// validate like any other and run through the same pipeline.
func ToolsFromCapability(inv CapabilityInvoker) ([]Tool, error) {
	m := inv.Manifest()
	if err := m.Validate(); err != nil {
		return nil, err
	}
	out := make([]Tool, 0, len(m.Operations))
	for _, op := range m.Operations {
		op := op
		out = append(out, &capabilityTool{inv: inv, op: op})
	}
	return out, nil
}

// capabilityTool adapts one capability operation to the Tool interface.
type capabilityTool struct {
	inv CapabilityInvoker
	op  capabilities.Operation
}

func (t *capabilityTool) Definition() Definition {
	timeout := time.Duration(120_000_000_000)
	if t.op.TimeoutSecs > 0 {
		timeout = time.Duration(t.op.TimeoutSecs) * time.Second
	}
	budget := int64(DefaultOutputBudget)
	if t.op.OutputBudget > 0 {
		budget = t.op.OutputBudget
	}
	group := t.op.Group
	if group == "" {
		group = "capability"
	}
	tags := append([]string(nil), t.op.Tags...)
	if len(tags) == 0 {
		tags = []string{"capability"}
	}
	keywords := append([]string{"capability", t.op.Name}, t.op.Keywords...)
	return Definition{
		Name: t.op.Name, Description: t.op.Description,
		InputSchema: t.op.InputSchema, OutputSchema: t.op.OutputSchema,
		Effect: types.EffectClass(t.op.Effect), Idempotency: types.IdempotencyClass(t.op.Idempotency),
		Cancel: types.CancelMode(t.op.Cancel), Fast: t.op.Fast,
		Timeout: timeout, OutputBudget: budget,
		Platforms: append([]string(nil), t.op.Platforms...),
		Profiles:  append([]string(nil), t.op.Profiles...),
		Inverse:   t.op.Inverse, Probe: t.op.Probe, DryRun: t.op.DryRun,
		Group: group, Tags: tags, Keywords: keywords,
	}
}

func (t *capabilityTool) Invoke(ctx context.Context, inv Invocation) (Result, error) {
	key := inv.IdempotencyKey
	if key == "" {
		key = "cap-" + inv.Tool + "-once"
	}
	first, err := t.inv.Invoke(t.op.Name, inv.Arguments, key)
	if err != nil {
		if capabilities.ClassifyError(err) == capabilities.ErrTransient {
			return Result{Status: StatusTransient, Transient: true, Output: err.Error()}, nil
		}
		return Result{Status: StatusToolError, Output: err.Error()}, nil
	}
	return t.follow(ctx, first)
}

func (t *capabilityTool) follow(ctx context.Context, first capabilities.TaskResult) (Result, error) {
	cur := first
	deadline := time.Now().Add(2 * time.Minute)
	for {
		switch cur.State {
		case capabilities.StateCompleted:
			out := string(cur.Payload)
			if out == "" {
				out = "{}"
			}
			return Result{Status: StatusOK, Output: out}, nil
		case capabilities.StateFailed:
			msg := cur.ErrorText
			if msg == "" {
				msg = string(cur.Payload)
			}
			if msg == "" {
				msg = "capability failed"
			}
			if cur.ErrClass == capabilities.ErrTransient {
				return Result{Status: StatusTransient, Transient: true, Output: msg}, nil
			}
			if cur.ErrClass == capabilities.ErrInput {
				return Result{Status: StatusInputError, Output: msg}, nil
			}
			return Result{Status: StatusToolError, Output: msg}, nil
		case capabilities.StateCancelled:
			return Result{Status: StatusCanceled, Output: "capability cancelled"}, nil
		case capabilities.StateInputRequired:
			// The engine turns this into ask_user (Section 14.1); the
			// tool surfaces the need deterministically.
			need := cur.Need
			if need == "" {
				need = string(cur.Payload)
			}
			return Result{Status: StatusOK, Output: "input_required: " + need}, nil
		case capabilities.StateWorking:
			select {
			case <-ctx.Done():
				_, _ = t.inv.Cancel(cur.TaskID)
				return Result{Status: StatusCanceled, Output: "capability poll canceled"}, nil
			default:
			}
			if time.Now().After(deadline) {
				return Result{Status: StatusTimeout, Output: fmt.Sprintf("capability %q still working", t.op.Name)}, nil
			}
			next, err := t.inv.Poll(cur.TaskID)
			if err != nil {
				return Result{Status: StatusToolError, Output: err.Error()}, nil
			}
			if next.TaskID == "" {
				next.TaskID = cur.TaskID
			}
			cur = next
		default:
			return Result{Status: StatusToolError, Output: fmt.Sprintf("unknown capability state %q", cur.State)}, nil
		}
	}
}

func (t *capabilityTool) ResourceKeys(args json.RawMessage) ([]string, error) {
	return []string{"capability:" + t.op.Name}, nil
}
