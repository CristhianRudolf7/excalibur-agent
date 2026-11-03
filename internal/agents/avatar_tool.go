package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// DesignAvatarTool validates a model-selected spec without persisting an agent.
// Description-only requests return a deterministic starting palette.
type DesignAvatarTool struct{}

func (*DesignAvatarTool) Definition() tools.Definition {
	properties := map[string]any{
		"variant":   map[string]any{"type": "string", "enum": avatarVariants},
		"accessory": map[string]any{"type": "string", "enum": avatarAccessories},
	}
	for _, field := range []string{"tone", "hair", "highlight", "outfit", "accent", "background"} {
		properties[field] = map[string]any{"type": "string", "pattern": `^#[0-9a-fA-F]{6}$`}
	}
	schema, _ := json.Marshal(map[string]any{
		"type": "object", "additionalProperties": false, "required": []string{"description"},
		"properties": map[string]any{
			"description": map[string]any{"type": "string", "minLength": 1, "maxLength": 2048},
			"spec": map[string]any{"type": "object", "additionalProperties": false,
				"required": []string{"variant", "tone", "hair", "highlight", "outfit", "accent", "accessory", "background"}, "properties": properties},
		},
	})
	return tools.Definition{
		Name: "design_avatar", Description: "Design an agent avatar in the 2D world pixel-art style. Translate the user's description into spec fields, then validate its palette and receive a short preview. Load agent-avatars for field guidance. Omitting spec returns a deterministic starting avatar, not a semantic interpretation.",
		InputSchema: schema, Effect: types.EffectReadOnly, Idempotency: types.IdemPure,
		Cancel: types.CancelImmediate, Timeout: time.Second, OutputBudget: 2048,
		Fast: true, Platforms: []string{"linux"}, Profiles: []string{"default", "delegate"},
		Group: "creative-tools", Tags: []string{"avatar", "pixel-art"},
		Keywords: []string{"avatar", "icon", "character", "appearance", "icono", "muñeco", "personaje", "apariencia"},
	}
}

func (*DesignAvatarTool) ResourceKeys(json.RawMessage) ([]string, error) { return nil, nil }

func (*DesignAvatarTool) Invoke(ctx context.Context, inv tools.Invocation) (tools.Result, error) {
	if err := ctx.Err(); err != nil {
		return tools.Result{Status: tools.StatusCanceled, Output: err.Error()}, nil
	}
	fail := func(err error) (tools.Result, error) {
		return tools.Result{Status: tools.StatusInputError, Output: err.Error()}, nil
	}
	if len(inv.Arguments) > 16<<10 || !json.Valid(inv.Arguments) {
		return fail(fmt.Errorf("avatar: invalid or oversized arguments"))
	}
	var args struct {
		Description string      `json:"description"`
		Spec        *AvatarSpec `json:"spec"`
	}
	d := json.NewDecoder(bytes.NewReader(inv.Arguments))
	d.DisallowUnknownFields()
	if err := d.Decode(&args); err != nil {
		return fail(err)
	}
	if strings.TrimSpace(args.Description) == "" || utf8.RuneCountInString(args.Description) > 2048 {
		return fail(fmt.Errorf("avatar: description must contain 1 to 2048 characters"))
	}
	spec := DefaultAvatar(args.Description)
	if args.Spec != nil {
		var err error
		spec, err = ValidateAvatar(*args.Spec)
		if err != nil {
			return fail(err)
		}
	}
	preview := fmt.Sprintf("Pixel-art %s avatar: skin %s, hair %s, outfit %s, highlight %s, accent %s, accessory %s, background %s.",
		spec.Variant, spec.Tone, spec.Hair, spec.Outfit, spec.Highlight, spec.Accent, spec.Accessory, spec.Background)
	raw, err := json.Marshal(struct {
		Spec    AvatarSpec `json:"spec"`
		Preview string     `json:"preview"`
	}{spec, preview})
	return tools.Result{Status: tools.StatusOK, Output: string(raw)}, err
}
