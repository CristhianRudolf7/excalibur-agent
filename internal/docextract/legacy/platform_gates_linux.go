package legacy

// Per-OS gate for legacy Office conversion. Each OS flips its own file.
// conversionSupported reports whether legacy formats can be converted.
func conversionSupported() bool { return true }
