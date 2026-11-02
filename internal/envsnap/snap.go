package envsnap

import (
	"context"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
)

// EnvProvider is the seam the tool-surface task programs against: one
// snapshot collected at task start, rendered once into L2. The engine
// and CLI paths depend on this interface, not on the collector, so the
// exec tool implementation can share the shape without a cycle.
type EnvProvider interface {
	// Snapshot returns the collected snapshot (collects on first call).
	Snapshot(ctx context.Context) Snapshot
	// RenderCompact formats the snapshot for L2 injection.
	RenderCompact() string
}

// NetworkMode names the reachability verdict.
type NetworkMode string

// Reachability verdicts.
const (
	NetworkOpen       NetworkMode = "open"
	NetworkRestricted NetworkMode = "restricted"
	NetworkUnknown    NetworkMode = "unknown"
)

// Snapshot is one best-effort environment pass. Empty strings and
// empty maps mean "not detected", never an error.
type Snapshot struct {
	At            time.Time         `json:"at"`
	OS            string            `json:"os"`
	Distro        string            `json:"distro,omitempty"`
	Arch          string            `json:"arch"`
	EffectiveCPUs float64           `json:"effective_cpus,omitempty"`
	HostCPUs      int               `json:"host_cpus,omitempty"`
	HostMemBytes  int64             `json:"host_mem_bytes,omitempty"`
	MemoryLimited bool              `json:"memory_limited,omitempty"`
	CPUs          int               `json:"cpus"`
	MemTotalBytes int64             `json:"mem_total_bytes,omitempty"`
	DiskFreeBytes int64             `json:"disk_free_bytes,omitempty"`
	Shells        []string          `json:"shells,omitempty"`
	DefaultShell  string            `json:"default_shell,omitempty"`
	PkgManagers   []string          `json:"pkg_managers,omitempty"`
	Languages     map[string]string `json:"languages,omitempty"`
	Compilers     map[string]string `json:"compilers,omitempty"`
	ToolsPresent  []string          `json:"tools_present,omitempty"`
	ToolsMissing  []string          `json:"tools_missing,omitempty"`
	Network       NetworkMode       `json:"network"`
	NetworkDetail string            `json:"network_detail,omitempty"`
	Workdir       string            `json:"workdir,omitempty"`
	Tree          string            `json:"tree,omitempty"`
	TreeTruncated bool              `json:"tree_truncated,omitempty"`
	Services      []string          `json:"services,omitempty"`
}

// versionedTools are probed with --version (name -> display name).
var versionedTools = []string{
	"python3", "python", "node", "nodejs", "npm", "go",
	"rustc", "cargo", "ruby", "java", "php", "perl",
	"Rscript", "dotnet", "julia", "gcc", "g++", "cc",
	"clang", "make", "cmake", "ninja", "git", "docker",
	"pip3", "uv", "yarn", "pnpm", "jq", "rg", "sqlite3",
}

// presenceTools are PATH-checked only (cheap, no subprocess).
var presenceTools = []string{
	"curl", "wget", "ssh", "tmux", "screen", "gh", "kubectl",
	"psql", "mysql", "redis-cli", "mvn", "gradle", "poetry",
	"fd", "fzf", "tar", "unzip", "zip", "ssh-keygen",
}

// packageManagers in probe order.
var packageManagers = []string{
	"apt", "apt-get", "dnf", "yum", "pacman", "apk", "brew", "nix",
	"pip3", "pip", "npm", "yarn", "pnpm", "cargo", "go",
}

// Collector gathers one snapshot with a bounded fan-out.
type Collector struct {
	cfg Config
}

// NewCollector returns a collector with normalized config.
func NewCollector(cfg Config) *Collector {
	return &Collector{cfg: cfg.Normalize()}
}

// Collect runs the bounded collection pass. It never returns an
// error: every probe degrades to absence, and the parent context
// bounds the whole pass to cfg.Timeout.
func (c *Collector) Collect(ctx context.Context) Snapshot {
	cfg := c.cfg
	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()
	snap := Snapshot{
		At:        time.Now().UTC(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
		CPUs:      runtime.NumCPU(),
		Languages: map[string]string{},
		Compilers: map[string]string{},
		Network:   NetworkUnknown,
		Workdir:   cfg.Workdir,
	}
	snap.Distro = detectDistro()
	snap.applyResources(ResourceReader{ProcRoot: cfg.ProcRoot, SysRoot: cfg.SysRoot}.Limits())
	if free, ok := detectDiskFree(cfg.Workdir); ok {
		snap.DiskFreeBytes = free
	}
	snap.Shells = detectShells()
	snap.DefaultShell = pickDefaultShell(snap.Shells)
	snap.PkgManagers = presentOnly(packageManagers)

	var wg sync.WaitGroup
	// Version probes fan out through safego so a panicking probe
	// cannot kill the daemon (Section 3.2); the context bounds them.
	var mu sync.Mutex
	present := map[string]string{}
	for _, name := range versionedTools {
		name := name
		if _, err := exec.LookPath(name); err != nil {
			continue
		}
		wg.Add(1)
		safego.Go("envsnap/probe", nil, func() {
			defer wg.Done()
			ver := probeVersion(ctx, cfg.ProbeTimeout, name)
			mu.Lock()
			present[name] = ver
			mu.Unlock()
		})
	}
	treeCh := make(chan treeResult, 1)
	safego.Go("envsnap/tree", nil, func() {
		treeCh <- collectTree(ctx, cfg.Workdir, cfg.MaxTreeEntries, cfg.MaxTreeDepth)
	})
	portsCh := make(chan []string, 1)
	safego.Go("envsnap/ports", nil, func() {
		portsCh <- listeningPorts()
	})
	wg.Wait()
	snap.Services = <-portsCh
	tr := <-treeCh
	snap.Tree = tr.text
	snap.TreeTruncated = tr.truncated

	for name, ver := range present {
		switch name {
		case "gcc", "g++", "cc", "clang":
			snap.Compilers[name] = ver
		case "python3", "python", "node", "nodejs", "npm", "go",
			"rustc", "cargo", "ruby", "java", "php", "perl",
			"Rscript", "dotnet", "julia", "pip3", "uv", "yarn", "pnpm":
			snap.Languages[name] = ver
		default:
			snap.ToolsPresent = append(snap.ToolsPresent, verOrName(name, ver))
		}
	}
	for _, name := range presenceTools {
		if _, err := exec.LookPath(name); err == nil {
			snap.ToolsPresent = append(snap.ToolsPresent, name)
		}
	}
	sort.Strings(snap.ToolsPresent)
	for _, name := range []string{"git", "docker", "curl", "make", "gcc", "python3", "node", "go"} {
		if _, err := exec.LookPath(name); err != nil {
			snap.ToolsMissing = append(snap.ToolsMissing, name)
		}
	}
	if cfg.NetworkProbe {
		snap.Network, snap.NetworkDetail = probeNetwork(ctx, cfg.ProbeHost)
	}
	return snap
}

// verOrName renders a probed tool as name or name+version.
func verOrName(name, ver string) string {
	if ver == "" {
		return name
	}
	return name + " " + ver
}

// presentOnly filters names to those on PATH, preserving order.
func presentOnly(names []string) []string {
	var out []string
	for _, n := range names {
		if _, err := exec.LookPath(n); err == nil {
			out = append(out, n)
		}
	}
	return out
}

// detectShells lists usable Bourne-compatible shells.
func detectShells() []string {
	var out []string
	for _, sh := range []string{"bash", "zsh", "fish", "sh", "dash"} {
		if _, err := exec.LookPath(sh); err == nil {
			out = append(out, sh)
		}
	}
	return out
}

// pickDefaultShell prefers bash, then zsh, then the SHELL env hint,
// then sh (Section 13.5: bash or zsh on Linux).
func pickDefaultShell(shells []string) string {
	have := map[string]bool{}
	for _, s := range shells {
		have[s] = true
	}
	for _, pref := range []string{"bash", "zsh", "sh"} {
		if have[pref] {
			if p, err := exec.LookPath(pref); err == nil {
				return p
			}
			return pref
		}
	}
	if sh := os.Getenv("SHELL"); sh != "" {
		if base := filepath.Base(sh); have[base] {
			return sh
		}
	}
	return "/bin/sh"
}

// applyResources keeps legacy integer CPU consumers conservative while the
// rendered snapshot preserves fractional quotas.
func (s *Snapshot) applyResources(l ResourceLimits) {
	s.CPUs = max(1, int(math.Floor(l.CPUs)))
	s.MemTotalBytes = l.MemoryBytes
	if !EffectiveLimitsEnabled() {
		return
	}
	s.EffectiveCPUs = l.CPUs
	s.MemoryLimited = l.MemoryLimited
	if l.CPUs != float64(l.HostCPUs) {
		s.HostCPUs = l.HostCPUs
	}
	if l.MemoryBytes != l.HostMemory {
		s.HostMemBytes = l.HostMemory
	}
}
