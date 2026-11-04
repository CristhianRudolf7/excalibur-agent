package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

type publicPreviewTool struct {
	manager  *previewManager
	backend  *DaemonBackend
	services *Services
}

func (t *publicPreviewTool) Definition() tools.Definition {
	return tools.Definition{Name: "public_preview", Group: "preview", Profiles: []string{"default"}, Effect: types.EffectCommunication, Idempotency: types.IdemNonIdempotent, Cancel: types.CancelCooperative, Timeout: 60 * time.Second, OutputBudget: 8000,
		Keywords:    []string{"public preview", "public link", "phone", "share", "publish", "preview", "enlace público", "celular", "móvil", "compartir", "publicar", "página web"},
		Description: `Publish temporary public website/app previews with Cloudflare Quick Tunnels. When the user asks for a page/app link to open on their phone or share, use publish and give the returned URL. For a small static site, pass files directly as a relative_path:content map (include index.html, at most 2 MiB total). For a delegated project, pass its static output directory using path, inside daemon previews/workspaces or a registered project/workspace root. This serves static files; build apps first. Alternatively expose an existing HTTP server on 127.0.0.1 with port. An explicit request for a public or Cloudflare link authorizes either mode; publish immediately without another question. The daemon verifies authorization from user text; confirmed is a legacy field and cannot grant permission. Without an explicit request, do not publish. Anyone with the link can view the published content. Never claim a public link without a successful publish. Return the actual URL and expiry to the user; do not suggest hosting elsewhere instead of using this tool. Default TTL is 2 hours, maximum 24 hours, configurable with ttl_seconds. list returns active previews; stop requires id and closes only the tunnel/server owned by this daemon. All previews stop on daemon shutdown.`,
		InputSchema: json.RawMessage(`{"type":"object","properties":{"action":{"enum":["publish","list","stop"]},"files":{"type":"object","additionalProperties":{"type":"string"}},"path":{"type":"string"},"port":{"type":"integer","minimum":1,"maximum":65535},"confirmed":{"type":"boolean"},"ttl_seconds":{"type":"integer","minimum":1,"maximum":86400},"id":{"type":"string"}},"required":["action"],"additionalProperties":false}`)}
}
func (t *publicPreviewTool) ResourceKeys(json.RawMessage) ([]string, error) {
	return []string{"public-previews"}, nil
}
func (t *publicPreviewTool) Invoke(ctx context.Context, inv tools.Invocation) (tools.Result, error) {
	var req previewRequest
	if err := strictLibraryParams(inv.Arguments, &req); err != nil {
		return previewResult(nil, err)
	}
	switch req.Action {
	case "list":
		return previewResult(map[string]any{"previews": t.manager.list()}, nil)
	case "stop":
		return previewResult(map[string]any{"stopped": req.ID}, t.manager.stopID(req.ID))
	case "publish":
		policy, _ := ctx.Value(deliveryPolicyKey{}).(deliveryPolicy)
		if !policy.PublicPreview {
			return previewResult(nil, fmt.Errorf("public preview requires an explicit user request for a public or Cloudflare link; no content was published"))
		}
		req.Confirmed = true
		t.manager.mu.Lock()
		dataDir := t.manager.dataDir
		t.manager.mu.Unlock()
		roots := []string{}
		if dataDir != "" {
			roots = append(roots, filepath.Join(dataDir, "previews"), filepath.Join(dataDir, "workspaces"))
		}
		if t.services.Projects != nil {
			for _, p := range t.services.Projects.List() {
				roots = append(roots, p.Roots...)
			}
		}
		if t.backend != nil && t.backend.Services != nil && t.backend.Services.WorkspaceRoots != nil {
			roots = append(roots, t.backend.Services.WorkspaceRoots(ctx)...)
		}
		value, err := t.manager.publish(ctx, req, roots)
		return previewResult(value, err)
	default:
		return previewResult(nil, fmt.Errorf("unknown public_preview action"))
	}
}
func wirePublicPreviewTool(b *DaemonBackend, s *Services) error {
	if s == nil || s.Tools == nil {
		return nil
	}
	if s.previews == nil {
		s.previews = newPreviewManager("")
	}
	if b != nil {
		b.previews = s.previews
	}
	if _, err := s.Tools.Get("public_preview"); err == nil {
		return nil
	}

	if err := s.Tools.Register(&publicPreviewTool{manager: s.previews, backend: b, services: s}); err != nil {
		return err
	}
	if s.Catalog != nil {
		s.Catalog.MarkDeferred("public_preview")
	}
	return nil
}

var _ tools.Tool = (*publicPreviewTool)(nil)
