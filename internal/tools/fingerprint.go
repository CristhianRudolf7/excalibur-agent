package tools

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// Fingerprint is the sha256 hex of file content (Section 13.6).
type Fingerprint string

// FingerprintOf returns the fingerprint of content.
func FingerprintOf(content []byte) Fingerprint {
	sum := sha256.Sum256(content)
	return Fingerprint(hex.EncodeToString(sum[:]))
}

// FingerprintFile reads path and returns its fingerprint.
func FingerprintFile(path string) (Fingerprint, []byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", nil, err
	}
	return FingerprintOf(data), data, nil
}

// MismatchInfo describes a fingerprint mismatch for the agent: what
// changed between the read and the write attempt (Section 13.6).
type MismatchInfo struct {
	Path        string
	Expected    Fingerprint
	Actual      Fingerprint
	Diff        string
	Region      string
	Mismatches  int
	ShouldScale bool
}

// FingerprintError is returned when an edit is refused because the
// file changed since it was read.
type FingerprintError struct {
	Info MismatchInfo
}

func (e *FingerprintError) Error() string {
	if e.Info.Actual == "" {
		return fmt.Sprintf("tools: %s no longer exists since it was read (recorded fp %s); re-create it with write_file or read the current path first", e.Info.Path, e.Info.Expected)
	}
	return fmt.Sprintf("tools: %s changed since read (expected fp %s, actual fp %s):\n%s"+
		"the file changed outside this run after it was read; review the current lines above, then retry the same call with expected_fp %q (or re-read the file first)",
		e.Info.Path, e.Info.Expected, e.Info.Actual, e.Info.Diff, e.Info.Actual)
}

// placeholderFingerprints are values models write in place of a real
// fingerprint: wildcards, copied error words and dashes. They mean that
// no fingerprint was supplied.
var placeholderFingerprints = map[string]bool{
	"*": true, "**": true, "unavailable": true, "none": true, "unknown": true, "-": true,
}

// CleanFingerprint returns the caller-supplied fingerprint, or "" when
// the value is empty or a placeholder, so that the harness-recorded
// fingerprint decides the check.
func CleanFingerprint(raw string) Fingerprint {
	s := strings.TrimSpace(raw)
	if s == "" || placeholderFingerprints[strings.ToLower(s)] {
		return ""
	}
	return Fingerprint(s)
}

// FingerprintStore tracks read fingerprints and consecutive mismatches
// per path. After MismatchScaleThreshold consecutive mismatches on the
// same file the execution must escalate to its parent (Section 13.6).
const MismatchScaleThreshold = 3

// FingerprintStore is the in-memory read-set of one execution.
type FingerprintStore struct {
	mu         sync.Mutex
	read       map[string]Fingerprint
	mismatches map[string]int
}

// NewFingerprintStore returns an empty store.
func NewFingerprintStore() *FingerprintStore {
	return &FingerprintStore{read: map[string]Fingerprint{}, mismatches: map[string]int{}}
}

// NoteRead records the fingerprint observed by read_file.
func (s *FingerprintStore) NoteRead(path string, fp Fingerprint) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.read[path] = fp
}

// LastRead returns the recorded fingerprint for path.
func (s *FingerprintStore) LastRead(path string) (Fingerprint, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fp, ok := s.read[path]
	return fp, ok
}

// Check compares the recorded (or caller-supplied expected) fingerprint
// with the current file. On mismatch it records the event and reports
// whether the caller must escalate.
func (s *FingerprintStore) Check(path string, expected Fingerprint) *MismatchInfo {
	s.mu.Lock()
	if expected == "" {
		expected = s.read[path]
	}
	s.mu.Unlock()
	if expected == "" {
		s.mu.Lock()
		s.mismatches[path] = 0
		s.mu.Unlock()
		return nil
	}
	fp, data, err := FingerprintFile(path)
	if err != nil {
		// Missing file against a recorded read is also a mismatch.
		return s.recordMismatch(path, expected, "", "", err.Error())
	}
	if fp == expected {
		s.mu.Lock()
		s.mismatches[path] = 0
		s.mu.Unlock()
		return nil
	}
	oldText := ""
	if prev, ok := s.lastContent(path, expected); ok {
		oldText = prev
	}
	diff := CompactDiff(oldText, string(data), path)
	return s.recordMismatch(path, expected, fp, diff, "")
}

func (s *FingerprintStore) recordMismatch(path string, expected, actual Fingerprint, diff, region string) *MismatchInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mismatches[path]++
	n := s.mismatches[path]
	return &MismatchInfo{
		Path: path, Expected: expected, Actual: actual,
		Diff: diff, Region: region,
		Mismatches: n, ShouldScale: n >= MismatchScaleThreshold,
	}
}

// Guard decides whether a write over an existing file may proceed
// (Section 13.6, automatic form). The recorded fingerprint is what this
// run last observed for path, from its own read or write; supplied is
// the caller's expected_fp after CleanFingerprint. The write proceeds
// when the file still matches the recorded state or the state the
// caller named. Otherwise the file changed outside this run since it
// was read, and the returned mismatch carries the current head and the
// fingerprint a retry can name. Nil means no check applies: nothing was
// recorded and nothing was supplied, or the check passed.
func (s *FingerprintStore) Guard(path string, supplied Fingerprint) *MismatchInfo {
	recorded, hasRecord := s.LastRead(path)
	if !hasRecord && supplied == "" {
		return nil
	}
	fp, data, err := FingerprintFile(path)
	expected := supplied
	if expected == "" {
		expected = recorded
	}
	if err != nil {
		return s.recordMismatch(path, expected, "", "", err.Error())
	}
	if (hasRecord && fp == recorded) || (supplied != "" && fp == supplied) {
		s.ResetMismatches(path)
		return nil
	}
	return s.recordMismatch(path, expected, fp, CompactDiff("", string(data), path), "")
}

// lastContent is a best-effort lookup of previously seen content; the
// executor keeps it simple and returns false so the diff falls back to
// the current region only.
func (s *FingerprintStore) lastContent(_ string, _ Fingerprint) (string, bool) {
	return "", false
}

// ResetMismatches clears the counter after a successful edit.
func (s *FingerprintStore) ResetMismatches(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mismatches[path] = 0
}

// MatchLevel reports which of the three coincidence levels matched
// (Section 13.6): exact, typographic-normalized, or trailing-space
// insensitive (OpenCode V2 levels).
type MatchLevel int

const (
	MatchExact MatchLevel = iota + 1
	MatchNormalized
	MatchTrimmed
)

// FindMatch locates oldText in content with the three levels. It
// returns the byte offset, level and all exact-or-best line numbers
// when ambiguous.
func FindMatch(content, oldText string) (offset int, level MatchLevel, ambiguous []int) {
	if oldText == "" {
		return -1, 0, nil
	}
	if idx := strings.Index(content, oldText); idx >= 0 {
		if n := strings.Count(content, oldText); n > 1 {
			return -1, 0, matchLines(content, oldText, stringsEqual)
		}
		return idx, MatchExact, nil
	}
	normContent, normMap := normalizeWithMap(content)
	normOld := normalizeText(oldText)
	if idx := strings.Index(normContent, normOld); idx >= 0 {
		orig := mapToOriginal(content, normMap, idx, len(normOld))
		if n := strings.Count(normContent, normOld); n > 1 {
			return -1, 0, matchLines(content, oldText, stringsEqualFoldNorm)
		}
		return orig, MatchNormalized, nil
	}
	trimmed := trimLines(content)
	trimOld := trimLines(oldText)
	if idx := strings.Index(trimmed, trimOld); idx >= 0 {
		if n := strings.Count(trimmed, trimOld); n > 1 {
			return -1, 0, matchLines(content, oldText, stringsEqualTrimmed)
		}
		// Map back approximately: find the first line-anchored match.
		lines := strings.Split(content, "\n")
		oldLines := strings.Split(oldText, "\n")
		for i := range lines {
			if i+len(oldLines) > len(lines) {
				break
			}
			ok := true
			for j, ol := range oldLines {
				if strings.TrimRight(lines[i+j], " \t\r") != strings.TrimRight(ol, " \t\r") {
					ok = false
					break
				}
			}
			if ok {
				off := 0
				for k := 0; k < i; k++ {
					off += len(lines[k]) + 1
				}
				return off, MatchTrimmed, nil
			}
		}
		return idx, MatchTrimmed, nil
	}
	return -1, 0, nil
}

func stringsEqual(a, b string) bool { return a == b }

func stringsEqualFoldNorm(a, b string) bool { return normalizeText(a) == normalizeText(b) }

func stringsEqualTrimmed(a, b string) bool {
	return strings.TrimRight(a, " \t\r") == strings.TrimRight(b, " \t\r")
}

func matchLines(content, oldText string, eq func(a, b string) bool) []int {
	cl := strings.Split(content, "\n")
	ol := strings.Split(oldText, "\n")
	var out []int
	for i := 0; i+len(ol) <= len(cl); i++ {
		ok := true
		for j := range ol {
			if !eq(cl[i+j], ol[j]) {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, i+1)
		}
	}
	return out
}

// normalizeText folds typographic characters to ASCII for level 2.
func normalizeText(s string) string {
	r := strings.NewReplacer(
		"\u2018", "'", "\u2019", "'", "\u201a", "'",
		"\u201c", `"`, "\u201d", `"`, "\u201e", `"`,
		"\u2013", "-", "\u2014", "-", "\u2212", "-",
		"\u00a0", " ", "\u2026", "...",
	)
	return r.Replace(s)
}

func normalizeWithMap(s string) (string, []int) {
	norm := normalizeText(s)
	// Folding is length-changing; build a best-effort index map by
	// aligning on the normalized string directly. For offset mapping we
	// keep identity when lengths match, else fall back to proportional.
	m := make([]int, len(norm)+1)
	if len(norm) == len(s) {
		for i := range m {
			m[i] = i
		}
		return norm, m
	}
	for i := range m {
		m[i] = i * len(s) / len(norm)
	}
	return norm, m
}

func mapToOriginal(orig string, m []int, idx, ln int) int {
	if idx < 0 || idx >= len(m) {
		return idx
	}
	off := m[idx]
	if off < 0 {
		off = 0
	}
	if off > len(orig) {
		off = len(orig)
	}
	_ = ln
	return off
}

func trimLines(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t\r")
	}
	return strings.Join(lines, "\n")
}

// CompactDiff renders a compact diff between the read-time view and
// the current content: changed line ranges with a few context lines.
func CompactDiff(oldText, newText, path string) string {
	oldLines := strings.Split(oldText, "\n")
	newLines := strings.Split(newText, "\n")
	if oldText == "" {
		// No recorded content: show the head of the current file as the
		// region the agent must reconcile with.
		shown := newLines
		if len(shown) > 20 {
			shown = shown[:20]
		}
		var b strings.Builder
		fmt.Fprintf(&b, "--- current %s (first %d of %d lines) ---\n", path, len(shown), len(newLines))
		for i, l := range shown {
			fmt.Fprintf(&b, "%4d %s\n", i+1, l)
		}
		return b.String()
	}
	// Simple LCS-free diff: find common prefix/suffix, show the middle
	// with 3 context lines.
	pre := 0
	for pre < len(oldLines) && pre < len(newLines) && oldLines[pre] == newLines[pre] {
		pre++
	}
	suf := 0
	for suf < len(oldLines)-pre && suf < len(newLines)-pre && oldLines[len(oldLines)-1-suf] == newLines[len(newLines)-1-suf] {
		suf++
	}
	var b strings.Builder
	fmt.Fprintf(&b, "--- %s changed (old lines %d..%d) ---\n", path, pre+1, len(oldLines)-suf)
	lo := pre - 3
	if lo < 0 {
		lo = 0
	}
	for i := lo; i < pre; i++ {
		fmt.Fprintf(&b, "  %4d %s\n", i+1, oldLines[i])
	}
	for i := pre; i < len(oldLines)-suf; i++ {
		fmt.Fprintf(&b, "- %4d %s\n", i+1, oldLines[i])
	}
	for i := pre; i < len(newLines)-suf; i++ {
		fmt.Fprintf(&b, "+ %4d %s\n", i+1, newLines[i])
	}
	hi := len(newLines) - suf + 3
	if hi > len(newLines) {
		hi = len(newLines)
	}
	for i := len(newLines) - suf; i < hi; i++ {
		fmt.Fprintf(&b, "  %4d %s\n", i+1, newLines[i])
	}
	return b.String()
}

// LeaseManager serializes writers per canonical path (Section 16.2).
// Acquire waits up to waitTimeout for the holder; leases expire after
// expireAfter without a heartbeat.
type LeaseManager interface {
	Acquire(path, holder string) (release func(), err error)
	Holding(path string) (holder string, ok bool)
}

// MemoryLeases is an in-process LeaseManager for tests and embedded
// use. Cross-process coordination additionally uses FileLock.
type MemoryLeases struct {
	mu       sync.Mutex
	holders  map[string]string
	expires  map[string]time.Time
	waitMax  time.Duration
	expireIn time.Duration
	now      func() time.Time
}

// NewMemoryLeases returns a manager with Section 16.2 timings
// (30 s wait, 10 min expiry) unless overridden.
func NewMemoryLeases() *MemoryLeases {
	return &MemoryLeases{
		holders:  map[string]string{},
		expires:  map[string]time.Time{},
		waitMax:  30 * time.Second,
		expireIn: 10 * time.Minute,
		now:      time.Now,
	}
}

// Acquire implements LeaseManager.
func (m *MemoryLeases) Acquire(path, holder string) (func(), error) {
	deadline := m.now().Add(m.waitMax)
	for {
		m.mu.Lock()
		h, ok := m.holders[path]
		if !ok || m.now().After(m.expires[path]) {
			m.holders[path] = holder
			m.expires[path] = m.now().Add(m.expireIn)
			m.mu.Unlock()
			return func() {
				m.mu.Lock()
				defer m.mu.Unlock()
				if m.holders[path] == holder {
					delete(m.holders, path)
					delete(m.expires, path)
				}
			}, nil
		}
		m.mu.Unlock()
		if m.now().After(deadline) {
			return nil, fmt.Errorf("tools: %s is leased by %s", path, h)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Holding implements LeaseManager.
func (m *MemoryLeases) Holding(path string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	h, ok := m.holders[path]
	if !ok || m.now().After(m.expires[path]) {
		return "", false
	}
	return h, true
}

// SortedKeys returns sorted map keys for deterministic errors.
func SortedKeys[T any](m map[string]T) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
