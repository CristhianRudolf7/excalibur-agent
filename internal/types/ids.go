package types

import (
	"crypto/rand"
	"fmt"
	"strings"
	"time"
)

// Prefix is the type tag prepended to every internal identifier.
// Identifiers are ULIDs (time-ordered, coordination-free) prefixed by
// type, as defined in Section 4.4.
type Prefix string

// Identifier prefixes from Section 4.4.
const (
	PrefixProject      Prefix = "prj_"
	PrefixConversation Prefix = "cnv_"
	PrefixMessage      Prefix = "msg_"
	PrefixTask         Prefix = "tsk_"
	PrefixRun          Prefix = "run_"
	PrefixAgent        Prefix = "agt_"
	PrefixRequirement  Prefix = "req_"
	PrefixCriterion    Prefix = "crt_"
	PrefixCheck        Prefix = "chk_"
	PrefixEvidence     Prefix = "evd_"
	PrefixMemory       Prefix = "mem_"
	PrefixSkill        Prefix = "skl_"
	PrefixJob          Prefix = "job_"
)

// allPrefixes lists every valid prefix for validation.
var allPrefixes = []Prefix{
	PrefixProject,
	PrefixConversation,
	PrefixMessage,
	PrefixTask,
	PrefixRun,
	PrefixAgent,
	PrefixRequirement,
	PrefixCriterion,
	PrefixCheck,
	PrefixEvidence,
	PrefixMemory,
	PrefixSkill,
	PrefixJob,
}

// crockford is the Crockford base32 alphabet used by ULID.
const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// ulidLen is the length of the random/time part of an identifier.
const ulidLen = 26

// NewULID returns a fresh ULID string: 48 bits of millisecond timestamp
// followed by 80 bits of cryptographic randomness, Crockford base32
// encoded. ULIDs sort lexicographically in creation order for distinct
// milliseconds.
func NewULID() string {
	var b [16]byte
	ms := uint64(time.Now().UTC().UnixMilli())
	b[0] = byte(ms >> 40)
	b[1] = byte(ms >> 32)
	b[2] = byte(ms >> 24)
	b[3] = byte(ms >> 16)
	b[4] = byte(ms >> 8)
	b[5] = byte(ms)
	if _, err := rand.Read(b[6:]); err != nil {
		panic(fmt.Sprintf("types: crypto/rand unavailable: %v", err))
	}
	// The 128-bit value is encoded as 26 groups of 5 bits with 2
	// leading zero bits (130 bits total); bit 0 is the MSB of b[0].
	var out [ulidLen]byte
	for k := 0; k < ulidLen; k++ {
		var v byte
		for i := 0; i < 5; i++ {
			n := 5*k + i // bit position in the 130-bit field
			v <<= 1
			if n >= 2 {
				d := n - 2 // bit position in the 128-bit value
				v |= (b[d/8] >> (7 - uint(d%8))) & 1
			}
		}
		out[k] = crockford[v]
	}
	return string(out[:])
}

// ULIDTime extracts the embedded millisecond timestamp from a ULID.
func ULIDTime(ulid string) (time.Time, error) {
	if len(ulid) != ulidLen {
		return time.Time{}, fmt.Errorf("types: bad ULID length %d", len(ulid))
	}
	var ms uint64
	for i := 0; i < 10; i++ {
		v := strings.IndexByte(crockford, ulid[i])
		if v < 0 {
			return time.Time{}, fmt.Errorf("types: bad ULID char %q", ulid[i])
		}
		ms = (ms << 5) | uint64(v)
	}
	// First char only carries 2 bits; drop the 2 overflow bits.
	ms &= (1 << 48) - 1
	return time.UnixMilli(int64(ms)).UTC(), nil
}

// NewID returns a new prefixed internal identifier, e.g. "tsk_…".
func NewID(p Prefix) string {
	return string(p) + NewULID()
}

// SplitID splits a prefixed identifier into its prefix and ULID parts.
func SplitID(id string) (Prefix, string, error) {
	for _, p := range allPrefixes {
		if rest, ok := strings.CutPrefix(id, string(p)); ok {
			if len(rest) != ulidLen {
				return "", "", fmt.Errorf("types: bad identifier %q: bad ULID length", id)
			}
			return p, rest, nil
		}
	}
	return "", "", fmt.Errorf("types: bad identifier %q: unknown prefix", id)
}

// ValidateID checks that id carries the expected prefix and a
// well-formed ULID body.
func ValidateID(id string, want Prefix) error {
	got, rest, err := SplitID(id)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("types: bad identifier %q: want prefix %q", id, want)
	}
	if _, err := ULIDTime(rest); err != nil {
		return fmt.Errorf("types: bad identifier %q: %w", id, err)
	}
	return nil
}
