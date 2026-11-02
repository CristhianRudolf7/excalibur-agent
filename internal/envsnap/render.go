package envsnap

import (
	"fmt"
	"sort"
	"strings"
)

// RenderCompact formats the snapshot as one bounded block for L2
// injection: facts the model would otherwise spend tool calls on, in
// compact form, with an explicit verify-before-trusting caveat. The
// output never exceeds MaxRenderBytes (default 1800).
//
// The block is collected once per run and rendered once, so it stays
// byte-stable for the whole run and never breaks the L0-L2 cache
// prefix (Section 11.1 rule 1: nothing variable before the last
// stable mark; the collection timestamp is deliberately omitted).
func (s Snapshot) RenderCompact(maxBytes int) string {
	if maxBytes <= 0 {
		maxBytes = DefaultConfig().MaxRenderBytes
	}
	var b strings.Builder
	b.WriteString("Environment snapshot (auto-detected at task start; verify with tools before relying on it):\n")
	fmt.Fprintf(&b, "os: %s", s.OS)
	if s.Distro != "" {
		b.WriteString(" (" + s.Distro + ")")
	}
	cpus := float64(s.CPUs)
	if s.EffectiveCPUs > 0 {
		cpus = s.EffectiveCPUs
	}
	fmt.Fprintf(&b, " arch=%s cpus=%g", s.Arch, cpus)
	if s.MemTotalBytes > 0 {
		fmt.Fprintf(&b, " mem=%s", humanBytes(s.MemTotalBytes))
	}
	if s.HostCPUs > 0 {
		fmt.Fprintf(&b, " (host cpus %d)", s.HostCPUs)
	}
	if s.HostMemBytes > 0 {
		fmt.Fprintf(&b, " (host mem %s)", MemoryLabel(s.HostMemBytes))
	}
	if s.DiskFreeBytes > 0 {
		fmt.Fprintf(&b, " diskfree=%s", humanBytes(s.DiskFreeBytes))
	}
	b.WriteString("\n")
	if s.MemoryLimited && EffectiveLimitsEnabled() {
		jobs := (ResourceLimits{CPUs: cpus, MemoryBytes: s.MemTotalBytes}).ParallelJobs()
		fmt.Fprintf(&b, "Build/test with at most %d parallel jobs; lower after an out-of-memory kill.\n", jobs)
	}
	if s.DefaultShell != "" {
		b.WriteString("shell: " + s.DefaultShell)
		if len(s.Shells) > 1 {
			rest := append([]string(nil), s.Shells...)
			sort.Strings(rest)
			b.WriteString(" (also: " + strings.Join(rest, ", ") + ")")
		}
		b.WriteString("\n")
	}
	if len(s.PkgManagers) > 0 {
		b.WriteString("pkg: " + strings.Join(s.PkgManagers, ", ") + "\n")
	}
	if langs := joinVersions(s.Languages); langs != "" {
		b.WriteString("languages: " + langs + "\n")
	}
	if comps := joinVersions(s.Compilers); comps != "" {
		b.WriteString("compilers: " + comps + "\n")
	}
	if len(s.ToolsPresent) > 0 {
		b.WriteString("tools present: " + strings.Join(s.ToolsPresent, ", ") + "\n")
	}
	if len(s.ToolsMissing) > 0 {
		b.WriteString("tools missing: " + strings.Join(s.ToolsMissing, ", ") + "\n")
	}
	switch s.Network {
	case NetworkOpen:
		b.WriteString("network: reachable (" + s.NetworkDetail + ")\n")
	case NetworkRestricted:
		b.WriteString("network: restricted (" + s.NetworkDetail + "); prefer local tools, do not retry the network in a loop\n")
	default:
		b.WriteString("network: unknown reachability; probe once before assuming offline\n")
	}
	if len(s.Services) > 0 {
		b.WriteString("listening: " + strings.Join(s.Services, ", ") + "\n")
	} else {
		b.WriteString("listening: no local TCP listeners detected\n")
	}
	dir := s.Workdir
	if dir == "" {
		dir = "."
	}
	b.WriteString("workdir " + dir + ":\n")
	if s.Tree != "" {
		b.WriteString(s.Tree)
		if !strings.HasSuffix(s.Tree, "\n") {
			b.WriteString("\n")
		}
	}
	out := b.String()
	if len(out) > maxBytes {
		out = out[:maxBytes-len("...[truncated]\n")] + "...[truncated]\n"
	}
	return out
}

// joinVersions renders name->version pairs in key order.
func joinVersions(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		if v := strings.TrimSpace(m[k]); v != "" {
			parts = append(parts, k+" "+shortenVersion(v))
		} else {
			parts = append(parts, k)
		}
	}
	return strings.Join(parts, ", ")
}

// shortenVersion keeps the first token of a --version line when it
// already carries the number (python3 3.12.1), else the whole short
// line.
func shortenVersion(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	fields := strings.Fields(v)
	if len(fields) <= 2 {
		return v
	}
	// "go version go1.24.1 linux/amd64" -> "go1.24.1".
	for _, f := range fields[1:] {
		if strings.ContainsAny(f, "0123456789") && !strings.Contains(f, "/") {
			return f
		}
	}
	return fields[0] + " " + fields[1]
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1fG", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.0fM", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0fK", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%dB", n)
	}
}
