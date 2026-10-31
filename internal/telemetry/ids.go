package telemetry

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// NewTraceID returns 32 lowercase hex chars.
func NewTraceID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// NewSpanID returns 16 lowercase hex chars.
func NewSpanID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// ValidateIDShapes checks trace/span id shapes without failing on
// randomness: lengths and hex alphabet only.
func ValidateIDShapes(traceID, spanID string) error {
	if len(traceID) != 32 || !isHex(traceID) {
		return fmt.Errorf("telemetry: bad trace id %q", traceID)
	}
	if len(spanID) != 16 || !isHex(spanID) {
		return fmt.Errorf("telemetry: bad span id %q", spanID)
	}
	return nil
}

func isHex(s string) bool {
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
