package cli

import (
	"github.com/CristhianRudolf7/excalibur-agent/internal/capabilities"
	"github.com/CristhianRudolf7/excalibur-agent/internal/config"
	"github.com/CristhianRudolf7/excalibur-agent/internal/wireengine"
)

// embeddedBrowserWeb is the admission boundary: eval never even probes Chrome,
// creates profile directories or resolves search credentials.
func embeddedBrowserWeb(profile ProfileName, opts ExecOptions, run *ExecRun, cfg config.Config) (*wireengine.BrowserWeb, error) {
	if profile == ProfileEval {
		return nil, nil
	}
	data, err := browserDataDir()
	if err != nil {
		return nil, err
	}
	var uploads []string
	if opts.ProjectDir != "" {
		uploads = append(uploads, opts.ProjectDir)
	}
	return wireengine.NewBrowserWeb(wireengine.BrowserWebOptions{Config: cfg, DataDir: data, Vault: opts.Vault, RunID: string(run.RunID), Project: opts.ProjectDir, Delegated: profile == ProfileDelegate, UploadDirs: uploads})
}
func embeddedBrowserCapability(r *wireengine.BrowserWeb) capabilities.Capability {
	if r.Browser != nil {
		return r.Browser
	}
	return capabilities.NewBrowserFake()
}
