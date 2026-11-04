package tools

// Per-OS gates for behaviour that shared files enable only where the
// platform backend exists. Each OS flips its own file.
// survivableSessionsSupported reports whether autonomous exec sessions
// can detach into an independent session that outlives the harness.
func survivableSessionsSupported() bool { return true }

// detachedBackgroundSupported reports whether background sessions can
// run detached with file-backed output.
func detachedBackgroundSupported() bool { return true }

// namespaceIsolationSupported reports whether diagram renderers may be
// probed for unshare-based network isolation.
func namespaceIsolationSupported() bool { return true }
