package daemon

import (
	"context"

	"github.com/CristhianRudolf7/excalibur-agent/internal/googleapi"
)

// googleAccount follows the daemon-owned provider, including a connection made
// after startup. It never copies credentials or changes the stable tool list.
type googleAccount struct{ services *Services }

func (g googleAccount) AccessToken(ctx context.Context) (string, error) {
	if g.services.Google == nil {
		return "", googleapi.ErrNotConnected
	}
	return g.services.Google.AccessToken(ctx)
}
func (g googleAccount) Status() googleapi.Status {
	if g.services.Google == nil {
		return googleapi.Status{}
	}
	return g.services.Google.Status()
}
func wireGoogleTools(b *DaemonBackend, s *Services) error {
	if s == nil || s.Tools == nil {
		return nil
	}
	if _, err := s.Tools.Get("gmail"); err == nil {
		return nil
	}
	opts := googleapi.ToolOptions{}
	if b.Services != nil {
		opts.Blobs = b.Services.FileBlobs
	}
	if err := googleapi.RegisterTools(s.Tools, googleAccount{s}, opts); err != nil {
		return err
	}
	if s.Catalog != nil {
		for _, name := range []string{"gmail", "drive", "docs"} {
			s.Catalog.MarkDeferred(name)
		}
	}
	return wirePublicPreviewTool(b, s)
}
