package wireengine

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/browser"
	"github.com/CristhianRudolf7/excalibur-agent/internal/capabilities"
	"github.com/CristhianRudolf7/excalibur-agent/internal/config"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/vault"
	"github.com/CristhianRudolf7/excalibur-agent/internal/websearch"
)

// BrowserWebOptions is explicitly supplied only by production assistant paths.
// A nil option keeps catalog construction hermetic and preserves eval's surface.
type BrowserWebOptions struct {
	Config         config.Config
	DataDir        string
	Blobs          browser.BlobStore
	Vault          *vault.Vault
	RunID, Project string
	Delegated      bool
	UploadDirs     []string
	Available      func() error // nil uses the system probe; tests inject availability.
	Warn           func(string)
}

// BrowserWeb owns the browser lifetime and the real web backend.
type BrowserWeb struct {
	Browser *browser.Engine
	Web     *tools.WebDeps
}

func (r *BrowserWeb) Close() error {
	if r == nil || r.Browser == nil {
		return nil
	}
	return r.Browser.Close()
}

// StoreBrowserBlobs adapts the existing durable store without a contract change.
type StoreBrowserBlobs struct{ Store store.BlobStore }

func (b StoreBrowserBlobs) Put(ctx context.Context, _ string, data []byte) (string, error) {
	return b.Store.Put(ctx, bytes.NewReader(data))
}

// NewBrowserWeb builds private collaborators without launching Chrome or resolving keys.
func NewBrowserWeb(o BrowserWebOptions) (*BrowserWeb, error) {
	if err := o.Config.Validate(); err != nil {
		return nil, err
	}
	warn := o.Warn
	if warn == nil {
		warn = func(s string) { slog.Warn(s) }
	}
	r := &BrowserWeb{}
	if o.Blobs == nil {
		o.Blobs = browserFiles{dir: filepath.Join(o.DataDir, "blobs", "browser")}
	}
	cfg := o.Config.Browser
	available := o.Available
	if available == nil {
		available = func() error { return browser.AvailableForChannel(cfg.Channel) }
	}
	switch {
	case !cfg.Enabled:
		warn("browser disabled: configuration")
	default:
		if err := available(); err != nil {
			warn("browser disabled: " + err.Error())
		} else {
			if o.DataDir == "" {
				return nil, errors.New("browser: data directory required")
			}
			dirs := []*string{&cfg.ProfileDir, &cfg.DownloadsDir, &cfg.RoutinesDir}
			for i, name := range []string{"profile", "downloads", "routines"} {
				if *dirs[i] == "" {
					*dirs[i] = filepath.Join(o.DataDir, "browser", name)
				}
				if err := os.MkdirAll(*dirs[i], 0700); err != nil {
					return nil, errors.New("browser: directory unavailable")
				}
				if err := os.Chmod(*dirs[i], 0700); err != nil {
					return nil, errors.New("browser: private directory unavailable")
				}
			}
			if !cfg.Sandbox {
				warn("browser: Chromium sandbox disabled by configuration")
			}
			uploads := append([]string(nil), o.UploadDirs...)
			uploads = append(uploads, cfg.DownloadsDir)
			r.Browser = browser.New(browser.Config{ProfileDir: cfg.ProfileDir, DownloadsDir: cfg.DownloadsDir, RoutinesDir: cfg.RoutinesDir, Headless: &cfg.Headless, Sandbox: &cfg.Sandbox, Channel: cfg.Channel, IdleTimeout: cfg.IdleTimeout, MaxTabs: cfg.MaxTabs, AllowedUploadDirs: uploads, Blobs: o.Blobs, Secrets: browserSecrets{o}})
		}
	}
	search := o.Config.Search
	var ps []websearch.ProviderConfig
	for _, name := range []string{"tavily", "brave"} {
		p, ok := search.Providers[name]
		if !ok {
			continue
		}
		ps = append(ps, websearch.ProviderConfig{Name: name, KeyRef: p.KeyRef, StorageRights: p.StorageRights})
	}
	var renderer websearch.Renderer
	if r.Browser != nil {
		renderer = &browserRenderer{Engine: r.Browser, UserAgent: search.UserAgent}
	}
	backend, err := websearch.New(websearch.Config{Durable: true, Order: search.Order, Providers: ps, UserAgent: search.UserAgent, Renderer: renderer, ResolveKey: func(ref string) websearch.KeyResolver { return searchKey(o, ref) }})
	if err != nil {
		_ = r.Close()
		return nil, err
	}
	for _, name := range search.Order {
		if name == "brave" && search.Providers[name].StorageRights {
			warn("Brave enabled for durable use: storage_rights=true (requires a Brave plan with storage rights)")
		}
	}
	r.Web = &tools.WebDeps{Backend: backend}
	return r, nil
}
func searchKey(o BrowserWebOptions, ref string) websearch.KeyResolver {
	return func(ctx context.Context) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if name, ok := strings.CutPrefix(ref, "env:"); ok {
			if val := os.Getenv(name); val != "" {
				return val, nil
			}
			return "", errors.New("search: credential unavailable")
		}
		if o.Vault == nil {
			return "", errors.New("search: vault unavailable")
		}
		res, err := o.Vault.Resolve(ctx, vault.ResolveRequest{Ref: ref, Tool: "web_search", RunID: o.RunID, Project: o.Project, Delegated: o.Delegated, Dest: vault.DestHeader})
		if err != nil {
			return "", errors.New("search: credential unavailable")
		}
		defer vault.ZeroBytes(res.Value)
		return string(res.Value), nil
	}
}

type browserSecrets struct{ options BrowserWebOptions }

func (s browserSecrets) Resolve(ctx context.Context, name, host string) (string, error) {
	o := s.options
	if o.Vault == nil {
		return "", errors.New("browser: vault unavailable")
	}
	// Delegated resolution must also retain project-only restrictions.
	var res vault.ResolveResult
	var err error
	if o.Delegated {
		res, err = o.Vault.Resolve(ctx, vault.ResolveRequest{Ref: name, Tool: "browser_act", RunID: o.RunID, Project: o.Project, Delegated: true, Domain: host, Dest: vault.DestForm})
	} else {
		res, err = o.Vault.CheckFormPolicy(ctx, name, "browser_act", o.RunID, o.Project, host)
	}
	if err != nil {
		return "", errors.New("browser: secret unavailable for this host")
	}
	defer vault.ZeroBytes(res.Value)
	return string(res.Value), nil
}

// SwapBrowser replaces just the capability; DefaultRegistry remains deterministic.
func (r *BrowserWeb) SwapBrowser(caps *capabilities.Registry) error {
	if r == nil || r.Browser == nil {
		return nil
	}
	caps.Unregister("browser")
	return caps.Register(r.Browser)
}

// WireBrowser preserves existing definitions and deferred membership. Embedded
// assistant runs add the existing manifest operations; eval never calls this.
func WireBrowser(emb *Embedded, cap capabilities.Capability) error {
	adapted, err := tools.ToolsFromCapability(cap)
	if err != nil {
		return err
	}
	for _, t := range adapted {
		if err := emb.Catalog.RegisterDeferred(t); err != nil {
			return err
		}
	}
	return nil
}
