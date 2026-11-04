package tasks

func init() {
	// Per-user temporary directories are writable task locations, even when
	// extraction runs before a workdir is available. Do not admit all of /var.
	DeliverableRoots = append(DeliverableRoots, "/var/folders", "/private/var/folders")
}
