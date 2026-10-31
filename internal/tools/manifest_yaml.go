package tools

import (
	"encoding/json"
	"fmt"

	"github.com/CristhianRudolf7/excalibur-agent/pkg/extsdk"
)

// YAML conversion for excalibur-extension.yaml (Section 14.12). The
// single YAML implementation lives in pkg/extsdk so manifests written
// against either side parse identically; this file only converts
// through the JSON view the parity test pins.

// loadManifestYAML parses excalibur-extension.yaml into the internal
// manifest: timeouts as duration strings or nanoseconds, schemas as
// YAML maps or inline JSON.
func loadManifestYAML(raw []byte) (Manifest, error) {
	sdkM, err := extsdk.UnmarshalManifestYAML(raw)
	if err != nil {
		return Manifest{}, err
	}
	conv, err := json.Marshal(sdkM)
	if err != nil {
		return Manifest{}, fmt.Errorf("tools: manifest convert: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(conv, &m); err != nil {
		return Manifest{}, fmt.Errorf("tools: manifest convert: %w", err)
	}
	return m, nil
}

// marshalManifestYAML renders the internal manifest as
// excalibur-extension.yaml.
func marshalManifestYAML(m Manifest) ([]byte, error) {
	conv, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("tools: manifest convert: %w", err)
	}
	var sdkM extsdk.Manifest
	if err := json.Unmarshal(conv, &sdkM); err != nil {
		return nil, fmt.Errorf("tools: manifest convert: %w", err)
	}
	return extsdk.MarshalManifestYAML(sdkM)
}
