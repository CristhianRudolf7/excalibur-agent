package cli

// os.CreateTemp already creates private files on this platform.
func protectInstructionFile(string) error { return nil }
