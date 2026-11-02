package daemon

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/config"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// FileConfig serves config.get/set/list over the layered loader
// (Section 9.2 project settings, Appendix B defaults): get returns
// the effective merged configuration, set deep-merges the supplied
// values and writes the user file, list names the top-level sections.
type FileConfig struct {
	// LoadOpts fixes the layer paths; zero value loads defaults.
	LoadOpts config.LoadOptions
	// UserPath is where set persists; empty derives from the loader.
	UserPath string
}

// configSectionNames mirrors the top-level config sections for list.
var configSectionNames = []string{
	"master", "engine", "porter", "provider", "context", "memory",
	"codeintel", "workspace", "skills", "tools", "models", "agenda",
	"capabilities", "telemetry", "vault", "gateway", "daemon", "eval",
	"browser", "search",
}

// Op routes one config.* gate call.
func (c *FileConfig) Op(method string, params json.RawMessage) (any, error) {
	switch method {
	case api.MethodConfigGet:
		cfg, err := config.Load(c.LoadOpts)
		if err != nil {
			return nil, &api.Error{Code: api.CodeInternal, Message: err.Error()}
		}
		return cfg, nil
	case api.MethodConfigList:
		return map[string]any{"sections": configSectionNames}, nil
	case api.MethodConfigSet:
		var p api.ConfigSetParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &api.Error{Code: api.CodeInvalidParams, Message: "bad config.set params"}
		}
		return c.set(p.Values)
	default:
		return nil, &api.Error{Code: api.CodeMethodNotFound, Message: "unknown method " + method}
	}
}

func (c *FileConfig) set(values map[string]any) (map[string]any, error) {
	if len(values) == 0 {
		return nil, &api.Error{Code: api.CodeInvalidParams, Message: "config.set needs values"}
	}
	cfg, err := config.Load(c.LoadOpts)
	if err != nil {
		return nil, &api.Error{Code: api.CodeInternal, Message: err.Error()}
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return nil, &api.Error{Code: api.CodeInternal, Message: err.Error()}
	}
	var merged map[string]any
	if err := json.Unmarshal(raw, &merged); err != nil {
		return nil, &api.Error{Code: api.CodeInternal, Message: err.Error()}
	}
	for k, v := range values {
		merged[strings.ToLower(k)] = v
	}
	mback, err := json.Marshal(merged)
	if err != nil {
		return nil, &api.Error{Code: api.CodeInternal, Message: err.Error()}
	}
	var next config.Config
	if err := json.Unmarshal(mback, &next); err != nil {
		return nil, &api.Error{Code: api.CodeInvalidParams, Message: fmt.Sprintf("config.set: values do not decode: %v", err)}
	}
	if err := next.Validate(); err != nil {
		return nil, &api.Error{Code: api.CodeInvalidParams, Message: fmt.Sprintf("config.set: invalid: %v", err)}
	}
	path := c.UserPath
	if path == "" {
		path = config.UserDefaultPath()
	}
	if err := config.WriteFile(path, next); err != nil {
		return nil, &api.Error{Code: api.CodeInternal, Message: err.Error()}
	}
	applied := make([]string, 0, len(values))
	for k := range values {
		applied = append(applied, strings.ToLower(k))
	}
	return map[string]any{"ok": true, "applied": applied}, nil
}

// mustProjectID converts a gate project id to the typed id.
func mustProjectID(id string) types.ProjectID {
	return types.ProjectID(id)
}
