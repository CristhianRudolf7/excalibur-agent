package wireengine

import (
	"github.com/CristhianRudolf7/excalibur-agent/internal/memory"
	"github.com/CristhianRudolf7/excalibur-agent/internal/models"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// ModelVisionMedia binds real image transport without changing tool metadata.
// visionModel is the optional explicit vision-role model. An empty value
// falls back to the configured default and produces a warning.
func ModelVisionMedia(p providers.Provider, cfg models.GatewayConfig, visionModel string) (*tools.MediaDeps, error) {
	entry := cfg.Roles[types.RoleExecutor]
	roles := map[types.ModelRole]models.RoleEntry{types.RoleExecutor: entry}
	if visionModel != "" {
		entry.Primary.Model = visionModel
		roles[types.RoleVision] = entry
	}
	cfg.Roles = roles
	backend, err := tools.NewModelVisionBackend(p, cfg, tools.VisionOptions{})
	if err != nil {
		return nil, err
	}
	return &tools.MediaDeps{Vision: backend}, nil
}

// VisionMemoryOption connects the same vision backend to library ingestion.
// With no backend, memory retains its honest unavailable behavior.
func VisionMemoryOption(media *tools.MediaDeps) memory.Option {
	if media == nil || media.Vision == nil {
		return memory.WithVision(nil)
	}
	return memory.WithVision(&memory.VisionToolAdapter{Tool: &tools.VisionExtractTool{Deps: media}})
}
