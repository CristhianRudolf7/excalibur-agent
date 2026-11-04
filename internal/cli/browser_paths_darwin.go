//go:build darwin

package cli

import "github.com/CristhianRudolf7/excalibur-agent/internal/platform"

// Use the daemon's data root even for project-scoped or ephemeral exec state:
// browser sessions share one persistent user profile.
func browserDataDir() (string, error) { return (platform.DarwinPaths{}).DataDir() }
