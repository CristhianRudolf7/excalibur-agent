package cli

import (
	"github.com/CristhianRudolf7/excalibur-agent/internal/config"
	"testing"
)

func TestEvalNeverBuildsRealBrowserOrSearch(t *testing.T) {
	// Invalid config and nil run would fail or panic if eval crossed admission.
	r, err := embeddedBrowserWeb(ProfileEval, ExecOptions{}, nil, config.Config{})
	if err != nil || r != nil {
		t.Fatalf("eval acquired production collaborators: %v", err)
	}
}

func TestEvalIgnoresBraveStorageRights(t *testing.T) {
	cfg := config.Default()
	p := cfg.Search.Providers["brave"]
	p.StorageRights = true
	cfg.Search.Providers["brave"] = p
	r, err := embeddedBrowserWeb(ProfileEval, ExecOptions{}, nil, cfg)
	if err != nil || r != nil {
		t.Fatalf("eval acquired opted-in Brave: %v", err)
	}
}
