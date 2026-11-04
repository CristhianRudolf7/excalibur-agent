package platform

// processParent is the portable part of a process snapshot.
type processParent struct{ PID, Parent uint32 }

// processTreePostorder includes the root and visits descendants first. A
// visited set also handles malformed snapshots and reused parent identifiers.
func processTreePostorder(root uint32, entries []processParent) []uint32 {
	children := make(map[uint32][]uint32)
	for _, e := range entries {
		children[e.Parent] = append(children[e.Parent], e.PID)
	}
	seen := make(map[uint32]bool)
	var order []uint32
	var visit func(uint32)
	visit = func(pid uint32) {
		if seen[pid] {
			return
		}
		seen[pid] = true
		for _, child := range children[pid] {
			visit(child)
		}
		order = append(order, pid)
	}
	visit(root)
	return order
}
