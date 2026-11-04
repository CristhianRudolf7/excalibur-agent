package main

// Per-OS gate for hiding the instruction from argv. Each OS flips its
// own file.
// argvReexecSupported reports whether exec may re-execute itself to
// remove the request from the process arguments.
func argvReexecSupported() bool { return true }
